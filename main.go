package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

const (
	BUFFERSIZE  = 8192
	DELAY       = 150
	MAXFILESIZE = 500 << 20 // 500 MB
	uploadDir   = "./uploads/"
)

type Connection struct {
	bufferChannel chan []byte
	buffer        []byte
}

type ConnectionPool struct {
	ConnectionMap map[*Connection]struct{}
	mu            sync.Mutex
}

func (cp *ConnectionPool) AddConnection(connection *Connection) {
	defer cp.mu.Unlock()
	cp.mu.Lock()
	cp.ConnectionMap[connection] = struct{}{}
}

func (cp *ConnectionPool) DeleteConnection(connection *Connection) {
	defer cp.mu.Unlock()
	cp.mu.Lock()
	delete(cp.ConnectionMap, connection)
}

func (cp *ConnectionPool) Broadcast(buffer []byte) {
	defer cp.mu.Unlock()
	cp.mu.Lock()
	for connection := range cp.ConnectionMap {
		copy(connection.buffer, buffer)
		select {
		case connection.bufferChannel <- connection.buffer:
		default:
		}
	}
}

func NewConnectionPool() *ConnectionPool {
	connectionMap := make(map[*Connection]struct{})
	return &ConnectionPool{ConnectionMap: connectionMap}
}

func stream(connectionPool *ConnectionPool, files []string) {
	for _, file := range files {
		filePath := filepath.Join(file)
		file, err := os.Open(filePath)
		if err != nil {
			log.Printf("Error opening file %s: %s", filePath, err)
			continue
		}

		buffer := make([]byte, BUFFERSIZE)
		for {
			clear(buffer)
			ticker := time.NewTicker(time.Millisecond * DELAY)
			for range ticker.C {
				n, err := file.Read(buffer)
				if err != nil && err != io.EOF {
					log.Printf("Error reading file %s: %s", filePath, err)
					ticker.Stop()
					break
				}
				if n == 0 {
					ticker.Stop()
					break
				}
				connectionPool.Broadcast(buffer)
			}
		}

		file.Close()
	}
}

func clear(buffer []byte) {
	for i := range buffer {
		buffer[i] = 0
	}
}

func main() {
	loadEnv()
	flag.Parse()
	port := os.Getenv("PORT")

	connPool := NewConnectionPool()

	files, err := filepath.Glob(uploadDir + "*")
	if err != nil {
		log.Fatal(err)
	}

	go stream(connPool, files)

	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseMultipartForm(MAXFILESIZE); err != nil {
			http.Error(w, "File size exceeds maximum limit", http.StatusBadRequest)
			return
		}

		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Error Retrieving the File", http.StatusBadRequest)
			return
		}
		defer file.Close()
		uuid := uuid.New()
		uploadFileName := filepath.Join(uploadDir, uuid.String()+"-"+handler.Filename)
		newFile, err := os.Create(uploadFileName)
		if err != nil {
			http.Error(w, "Error creating the file", http.StatusInternalServerError)
			return
		}
		defer newFile.Close()

		_, err = io.Copy(newFile, file)
		if err != nil {
			http.Error(w, "Error writing to the file", http.StatusInternalServerError)
			return
		}

		// Stop existing streams
		for connection := range connPool.ConnectionMap {
			close(connection.bufferChannel)
		}

		// Clear connection pool
		connPool.ConnectionMap = make(map[*Connection]struct{})

		files, err := filepath.Glob(uploadDir + "*")
		if err != nil {
			log.Fatal(err)
		}

		// Start streaming all files
		go stream(connPool, files)

		fileURL := fmt.Sprintf("%s/stream?path=%s", os.Getenv("IP"), uuid.String()+"-"+handler.Filename)
		data := map[string]interface{}{
			"url": fileURL,
		}
		jsonData, err := json.Marshal(data)
		if err != nil {
			http.Error(w, "Error converting to json", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "data: %s\n", jsonData)

	})

	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path parameter", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "audio/aac")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			log.Println("Could not create flusher")
			return
		}

		connection := &Connection{bufferChannel: make(chan []byte), buffer: make([]byte, BUFFERSIZE)}
		connPool.AddConnection(connection)
		log.Printf("%s has connected to the audio stream\n", r.Host)

		defer connPool.DeleteConnection(connection)

		for {
			buf := <-connection.bufferChannel
			if _, err := w.Write(buf); err != nil {
				log.Printf("%s's connection to the audio stream has been closed\n", r.Host)
				return
			}
			flusher.Flush()
			clear(connection.buffer)
		}
	})

	log.Println(fmt.Sprintf("Listening on port %s...", port))
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}
