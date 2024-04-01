package main

import (
    "bytes"
    "flag"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "sync"
    "time"
    "github.com/joho/godotenv"
)

func loadEnv() {
    err := godotenv.Load()
    if err != nil {
        log.Fatal("Error loading .env file")
    }
}

const (
    BUFFERSIZE   = 8192
    DELAY        = 150
    MAXFILESIZE  = 500 << 20 // 500 MB
    uploadDir    = "./uploads/"
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

func stream(connectionPool *ConnectionPool, filePath string) {
    file, err := os.Open(filePath)
    if err != nil {
        log.Printf("Error opening file %s: %s", filePath, err)
        return
    }
    defer file.Close()

    buffer := make([]byte, BUFFERSIZE)
    for {
        clear(buffer)
        ticker := time.NewTicker(time.Millisecond * DELAY)
        for range ticker.C {
            n, err := file.Read(buffer)
            if err != nil && err != io.EOF {
                log.Printf("Error reading file %s: %s", filePath, err)
                ticker.Stop()
                return
            }
            if n == 0 {
                ticker.Stop()
                break
            }
            connectionPool.Broadcast(buffer)
        }
    }
}

func clear(buffer []byte) {
    for i := range buffer {
        buffer[i] = 0
    }
}

func main() {
    loadEnv()
    fname := flag.String("filename", "", "path of the audio file")
    flag.Parse()

    var content []byte
    port := os.Getenv("PORT")
    if *fname != "" {
        file, err := os.Open(*fname)
        if err != nil {
            log.Fatal(err)
        }
        defer file.Close()

        fileInfo, err := file.Stat()
        if err != nil {
            log.Fatal(err)
        }

        if fileInfo.Size() > MAXFILESIZE {
            log.Fatal("File size exceeds maximum limit")
        }

        ctn, err := io.ReadAll(file)
        if err != nil {
            log.Fatal(err)
        }

        content = ctn
    }

    connPool := NewConnectionPool()

    if content != nil {
        go stream(connPool, *fname)
    }

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

        file, handler, err := r.FormFile("myFile")
        if err != nil {
            http.Error(w, "Error Retrieving the File", http.StatusBadRequest)
            return
        }
        defer file.Close()

        uploadFileName := filepath.Join(uploadDir, handler.Filename)
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

        go stream(connPool, uploadFileName)

        fileURL := fmt.Sprintf("%s/stream?path=%s", os.Getenv("IP"), handler.Filename)
        fmt.Fprintf(w, "Successfully Uploaded and Started Streaming File\nFile URL: %s\n", fileURL)
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
