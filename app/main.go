package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	s := Server{}
	s.Start()
}

type Server struct {
	listener net.Listener
}

func (s *Server) Start() {
	s.Listen()
	defer s.Close()
	fmt.Println("listening on 0.0.0.0:4221")

	// Serve one connection (enough for early stages).
	// Switch to a loop + goroutines later.
	conn := s.Accept()
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	path, headers, err := readRequestAndGetPathAndHeaders(conn)
	if err != nil {
		// Malformed request → 400
		resp := "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
		return
	}
	fmt.Println("Accepted path:", path)

	// Handle different paths
	if path == "/" {
		// Minimal valid HTTP response for root path
		body := "OK\n"
		resp := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n%s",
			len(body), body,
		)
		_, _ = conn.Write([]byte(resp))
	} else if strings.HasPrefix(path, "/echo/") {
		// Handle /echo/{str} endpoint
		str := strings.TrimPrefix(path, "/echo/")
		resp := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s",
			len(str), str,
		)
		_, _ = conn.Write([]byte(resp))
	} else if path == "/user-agent" {
		// Handle /user-agent endpoint
		userAgent := headers["User-Agent"]
		resp := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s",
			len(userAgent), userAgent,
		)
		_, _ = conn.Write([]byte(resp))
	} else {
		// Return 404 for any other path
		resp := "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
		_, _ = conn.Write([]byte(resp))
	}
}

func readRequestAndGetPathAndHeaders(conn net.Conn) (string, map[string]string, error) {
	r := bufio.NewReader(conn)

	// Request line: METHOD SP PATH SP VERSION CRLF
	reqLine, err := r.ReadString('\n')
	if err != nil {
		return "", nil, err
	}
	reqLine = strings.TrimRight(reqLine, "\r\n")
	parts := strings.Fields(reqLine)
	if len(parts) != 3 {
		return "", nil, fmt.Errorf("bad request line")
	}
	method, path, version := parts[0], parts[1], parts[2]
	if !strings.HasPrefix(version, "HTTP/") {
		return "", nil, fmt.Errorf("not http")
	}
	_ = method // not used yet, but parsed for future stages

	// Read headers until blank line
	headers := make(map[string]string)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", nil, err
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
	return path, headers, nil
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
