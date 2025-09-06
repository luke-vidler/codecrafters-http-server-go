package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	var directory string

	// Parse command line arguments
	for i, arg := range os.Args {
		if arg == "--directory" && i+1 < len(os.Args) {
			directory = os.Args[i+1]
			break
		}
	}

	s := Server{directory: directory}
	s.Start()
}

type Server struct {
	listener  net.Listener
	directory string
}

func (s *Server) Start() {
	s.Listen()
	defer s.Close()
	fmt.Println("listening on 0.0.0.0:4221")

	// Handle multiple concurrent connections
	for {
		conn := s.Accept()
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	for {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		method, path, headers, reader, err := readRequestAndGetMethodPathAndHeaders(conn)
		if err != nil {
			// Connection closed or malformed request, exit loop
			return
		}
		fmt.Println("Accepted path:", path)

		// Check if client wants to close connection
		connectionHeader := headers["Connection"]
		shouldClose := strings.ToLower(connectionHeader) == "close"

		// Handle different paths
		if path == "/" {
			// Minimal valid HTTP response for root path
			body := "OK\n"
			resp := fmt.Sprintf(
				"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: text/plain\r\n\r\n%s",
				len(body), body,
			)
			_, _ = conn.Write([]byte(resp))
		} else if strings.HasPrefix(path, "/echo/") {
			// Handle /echo/{str} endpoint
			str := strings.TrimPrefix(path, "/echo/")

			// Check if client supports gzip compression
			acceptEncoding := headers["Accept-Encoding"]
			supportsGzip := strings.Contains(acceptEncoding, "gzip")

			if supportsGzip {
				// Client supports gzip, compress the response body
				var buf bytes.Buffer
				gzipWriter := gzip.NewWriter(&buf)
				_, err := gzipWriter.Write([]byte(str))
				if err != nil {
					resp := "HTTP/1.1 500 Internal Server Error\r\n\r\n"
					_, _ = conn.Write([]byte(resp))
					return
				}
				err = gzipWriter.Close()
				if err != nil {
					resp := "HTTP/1.1 500 Internal Server Error\r\n\r\n"
					_, _ = conn.Write([]byte(resp))
					return
				}

				compressedData := buf.Bytes()

				// Send response headers
				respHeader := fmt.Sprintf(
					"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Encoding: gzip\r\nContent-Length: %d\r\n\r\n",
					len(compressedData),
				)
				_, _ = conn.Write([]byte(respHeader))

				// Send compressed body
				_, _ = conn.Write(compressedData)
			} else {
				// Client doesn't support gzip, send standard response
				resp := fmt.Sprintf(
					"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s",
					len(str), str,
				)
				_, _ = conn.Write([]byte(resp))
			}
		} else if path == "/user-agent" {
			// Handle /user-agent endpoint
			userAgent := headers["User-Agent"]
			resp := fmt.Sprintf(
				"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s",
				len(userAgent), userAgent,
			)
			_, _ = conn.Write([]byte(resp))
		} else if strings.HasPrefix(path, "/files/") {
			// Handle /files/{filename} endpoint
			filename := strings.TrimPrefix(path, "/files/")
			if method == "GET" {
				s.handleFileGetRequest(conn, filename)
			} else if method == "POST" {
				s.handleFilePostRequest(conn, filename, headers, reader)
			} else {
				// Method not allowed
				resp := "HTTP/1.1 405 Method Not Allowed\r\n\r\n"
				_, _ = conn.Write([]byte(resp))
			}
		} else {
			// Return 404 for any other path
			resp := "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"
			_, _ = conn.Write([]byte(resp))
		}

		// Close connection if requested by client
		if shouldClose {
			return
		}
	}
}

func (s *Server) handleFileGetRequest(conn net.Conn, filename string) {
	if s.directory == "" {
		// No directory specified, return 404
		resp := "HTTP/1.1 404 Not Found\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}

	// Construct full file path
	filePath := filepath.Join(s.directory, filename)

	// Check if file exists and read it
	file, err := os.Open(filePath)
	if err != nil {
		// File doesn't exist or can't be opened, return 404
		resp := "HTTP/1.1 404 Not Found\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}
	defer file.Close()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		resp := "HTTP/1.1 404 Not Found\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}

	// Send response headers
	resp := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\n\r\n",
		fileInfo.Size(),
	)
	_, _ = conn.Write([]byte(resp))

	// Send file contents
	_, _ = io.Copy(conn, file)
}

func (s *Server) handleFilePostRequest(conn net.Conn, filename string, headers map[string]string, reader *bufio.Reader) {
	if s.directory == "" {
		// No directory specified, return 404
		resp := "HTTP/1.1 404 Not Found\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}

	// Get content length
	contentLengthStr, ok := headers["Content-Length"]
	if !ok {
		resp := "HTTP/1.1 400 Bad Request\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}

	contentLength, err := strconv.Atoi(contentLengthStr)
	if err != nil || contentLength < 0 {
		resp := "HTTP/1.1 400 Bad Request\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}

	// Read request body
	body := make([]byte, contentLength)
	_, err = io.ReadFull(reader, body)
	if err != nil {
		resp := "HTTP/1.1 400 Bad Request\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}

	// Create file path
	filePath := filepath.Join(s.directory, filename)

	// Create and write file
	file, err := os.Create(filePath)
	if err != nil {
		resp := "HTTP/1.1 500 Internal Server Error\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}
	defer file.Close()

	_, err = file.Write(body)
	if err != nil {
		resp := "HTTP/1.1 500 Internal Server Error\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}

	// Return 201 Created
	resp := "HTTP/1.1 201 Created\r\n\r\n"
	_, _ = conn.Write([]byte(resp))
}

func readRequestAndGetMethodPathAndHeaders(conn net.Conn) (string, string, map[string]string, *bufio.Reader, error) {
	r := bufio.NewReader(conn)

	// Request line: METHOD SP PATH SP VERSION CRLF
	reqLine, err := r.ReadString('\n')
	if err != nil {
		return "", "", nil, nil, err
	}
	reqLine = strings.TrimRight(reqLine, "\r\n")
	parts := strings.Fields(reqLine)
	if len(parts) != 3 {
		return "", "", nil, nil, fmt.Errorf("bad request line")
	}
	method, path, version := parts[0], parts[1], parts[2]
	if !strings.HasPrefix(version, "HTTP/") {
		return "", "", nil, nil, fmt.Errorf("not http")
	}

	// Read headers until blank line
	headers := make(map[string]string)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", "", nil, nil, err
		}
		if line == "\r\n" { // end of headers
			break
		}
		// Parse header: Name: Value
		line = strings.TrimRight(line, "\r\n")
		colonIndex := strings.Index(line, ":")
		if colonIndex > 0 {
			name := strings.TrimSpace(line[:colonIndex])
			value := strings.TrimSpace(line[colonIndex+1:])
			headers[name] = value
		}
	}
	return method, path, headers, r, nil
}

func (s *Server) Listen() {
	l, err := net.Listen("tcp", "0.0.0.0:4221")
	if err != nil {
		fmt.Println("Failed to bind to port 4221")
		os.Exit(1)
	}
	s.listener = l
}

func (s *Server) Accept() net.Conn {
	conn, err := s.listener.Accept()
	if err != nil {
		fmt.Println("Error accepting connection:", err.Error())
		os.Exit(1)
	}
	fmt.Println("Accepted connection from:", conn.RemoteAddr())
	return conn
}

func (s *Server) Close() {
	if err := s.listener.Close(); err != nil {
		fmt.Println("Failed to close listener:", err.Error())
	}
}
