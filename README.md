# Audio Streaming Server

This is a simple audio streaming server implemented in Go. It allows users to upload audio files and stream them in real-time over HTTP.

## Features

- Upload audio files in various formats.
- Stream uploaded audio files in real-time.
- Supports multiple connections for concurrent streaming.

## Installation

1. Clone the repository:

   git clone https://github.com/your_username/audio-streaming-server.git

2. Navigate to the project directory:

   cd audio-streaming-server

3. Install dependencies:

   go mod tidy

4. Build the project:

   go build

5. Run the server:

   ./audio-streaming-server

By default, the server listens on port 8080.

## Usage

### Uploading Files

To upload an audio file, send a POST request to /upload endpoint with the audio file in the form data. For example:

   curl -X POST -F "myFile=@/path/to/audiofile.mp3" http://localhost:8080/upload

### Streaming Files

To stream an uploaded audio file, send a GET request to /stream endpoint with the path parameter specifying the filename. For example:

   http://localhost:8080/stream?path=audiofile.mp3

## Configuration

You can configure the server by setting environment variables:

- PORT: Port number for the server to listen on (default is 8080).
- IP: IP address of the server.

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
