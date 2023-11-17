package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var Processed string

type Config struct {
	Username  string `json:"username"`
	Webhook   string `json:"webhook"`
	WatchPath string `json:"watch_path"`
}

type Result struct {
	Title     string
	Rank      string
	Type      string
	Timestamp string
}

func readConfig() Config {
	configJsonFile, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(configJsonFile, &config); err != nil {
		log.Fatal(err)
	}
	return config
}

func analysis(filename string) (Result, error) {
	ext := filepath.Ext(filename)
	trimExt := strings.TrimSuffix(filename, ext)

	// タイムスタンプ削除
	pattern := `(?:\d+_){2}`
	re := regexp.MustCompile(pattern)
	if !re.MatchString(trimExt) {
		return Result{}, errors.New("リザルトじゃないからスルーしますの")
	}
	matches := re.FindAllStringSubmatch(trimExt, -1)
	timestamp := matches[0][0]
	str := re.ReplaceAllString(trimExt, "")
	// RANK
	pattern = `\s[ABCDEF]+$`
	re = regexp.MustCompile(pattern)
	if !re.MatchString(str) {
		return Result{}, errors.New("リザルトじゃないからスルーしますの")
	}
	matches = re.FindAllStringSubmatch(str, -1)
	rank := matches[0][0]
	str = re.ReplaceAllString(str, "")
	// Clear type
	pattern = `PERFECT|FULL\sCOMBO|LIGHT\sASSIST\sEASY\sCLEAR|EXHARD\sCLEAR|HARD\sCLEAR|EASY\sCLEAR|FAILED|CLEAR$`
	re = regexp.MustCompile(pattern)
	if !re.MatchString(str) {
		return Result{}, errors.New("リザルトじゃないからスルーしますの")
	}
	matches = re.FindAllStringSubmatch(str, -1)
	clearType := matches[0][0]
	title := re.ReplaceAllString(str, "")
	fmt.Println(title)

	return Result{strings.TrimSpace(title), strings.TrimSpace(rank), clearType, timestamp}, nil
}

func notify(config Config, eventName string) {
	if Processed == eventName {
		return
	}
	filename := filepath.Base(eventName)
	result, err := analysis(filename)
	if err != nil {
		fmt.Println(err)
		return
	}
	uploadFilename := result.Timestamp + filepath.Ext(filename)

	// ファイルを読み込み
	file, err := os.Open(eventName) // 送信したいファイル名に変更してください
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()

	// ファイルサイズをチェック
	info, err := file.Stat()
	if err != nil {
		fmt.Println(err)
		return
	}
	size64 := info.Size()
	var size int
	if int64(int(size64)) == size64 {
		size = int(size64)
	}
	if size == 0 {
		return
	}

	// リクエスト作成
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// ファイルのフィールドを作成
	part, err := writer.CreateFormFile("file", uploadFilename) // ファイルフィールドの名前とファイル名を指定してください
	if err != nil {
		fmt.Println(err)
		return
	}
	// ファイル内容を書き込み
	_, err = io.Copy(part, file)
	if err != nil {
		fmt.Println(err)
		return
	}

	// 他のフォームデータがある場合はここで追加
	payload, err := writer.CreateFormField("payload_json")
	if err != nil {
		fmt.Println(err)
		return
	}

	jsonData := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title": result.Title,
				"color": 0x00ff00,
				"image": map[string]interface{}{
					"url": "attachment://" + uploadFilename,
				},
				"fields": []interface{}{
					map[string]interface{}{
						"name":  ":trophy: Clear Type",
						"value": result.Type,
					},
					map[string]interface{}{
						"name":  ":military_medal: Rank",
						"value": result.Rank,
					},
				},
			},
		},
	}
	// JSONデータをバイト列にエンコード
	jsonValue, err := json.Marshal(jsonData)
	if err != nil {
		fmt.Println("JSONエンコードエラー:", err)
		return
	}

	if _, err = payload.Write(jsonValue); err != nil {
		fmt.Println(err)
		return
	}

	// マルチパートの最後を追加
	err = writer.Close()
	if err != nil {
		fmt.Println(err)
		return
	}

	// リクエストを作成
	req, err := http.NewRequest("POST", config.Webhook, body)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Content-Typeヘッダをセット
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// HTTPリクエスト送信
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	// レスポンスを出力
	fmt.Println("Status:", resp.Status)
	Processed = eventName

	// DEBUG用
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println(string(respBody))
}

func main() {
	// Read config.
	config := readConfig()
	log.Println("監視対象フォルダ: ", config.WatchPath)

	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Println("event:", event)
				if event.Has(fsnotify.Create) {
					time.Sleep(2000 * time.Millisecond)
				}
				if event.Has(fsnotify.Write) {
					notify(config, event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	// Add a path.
	err = watcher.Add(config.WatchPath)
	if err != nil {
		log.Fatal(err)
	}

	// Block main goroutine forever.
	<-make(chan struct{})
}
