package main

import (
	"bufio"
	"code.google.com/p/go.net/websocket"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"runtime/pprof"
	// "time"
)

// hello world, the web server
func indexCallback(w http.ResponseWriter, req *http.Request) {
	io.WriteString(w, html)
}

func debugCallback(name string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		g := pprof.Lookup(name)
		if g != nil {
			g.WriteTo(w, 2)
		} else {
			io.WriteString(w, html)
		}
	}
}

const (
	SESSION = iota
	SIZE
	CONTENT
)

type Session struct {
	SessionID uint32
	Request   chan []byte
	data      []byte
}

// session implement io.Reader
func (s *Session) Read(p []byte) (int, error) {
	if len(s.data) >= len(p) {
		copy(p, s.data)
		s.data = s.data[len(p):]
		return len(p), nil
	}

	copy(p, s.data)
	data, ok := <-s.Request
	if !ok {
		return len(s.data), errors.New("read error: channel closed!")
	}
	s.data = data
	log.Println("run here...")
	n, err := s.Read(p[len(s.data):])
	if err != nil {
		return n + len(s.data), err
	}

	return n + len(s.data), nil
}

func websocketCallback(ws *websocket.Conn) {
	table := make(map[uint32]*Session)

	var buf [8192]byte

	state := SESSION //读头部；读内容
	remain := 4      //剩余字节数
	readn := 0

	var session uint32
	var size uint16

	for {
		for readn != remain {
			n, err := ws.Read(buf[readn:remain])
			if err != nil {
				log.Println("Read session error: ", err)
			}
			readn += n
		}

		switch state {
		case SESSION:
			session = binary.LittleEndian.Uint32(buf[:])
			state = SIZE
			remain = 2
			readn = 0
		case SIZE:
			size = binary.LittleEndian.Uint16(buf[:])
			state = CONTENT
			remain = int(size)
			readn = 0
		case CONTENT:
			s, ok := table[session]
			if !ok {
				s = new(Session)
				s.SessionID = session
				s.Request = make(chan []byte)
				table[session] = s
				go func(ws *websocket.Conn, session *Session) {
					bufreader := bufio.NewReader(session)
					req, err := http.ReadRequest(bufreader)
					if err != nil {
						log.Println("read request error:", err)
						return
					}
					log.Println("parse a request:", req)

					var client http.Client
					resp, err := client.Do(req)
					if err != nil {
						log.Println("client.Do error:", err)
						return
					}

					resp.Write(os.Stdout)
				}(ws, s)
			}

			s.Request <- buf[:size]
			log.Println("receive a request: ", session, string(buf[:size]))

			state = SESSION
			remain = 4
			readn = 0
		}
	}
}

func main() {
	// loop()
	http.HandleFunc("/", indexCallback)
	http.Handle("/websocket", websocket.Handler(websocketCallback))
	http.HandleFunc("/pprof/goroutine", debugCallback("goroutine"))
	http.HandleFunc("/pprof/heap", debugCallback("heap"))
	http.HandleFunc("/pprof/threadcreate", debugCallback("threadcreate"))
	http.HandleFunc("/pprof/block", debugCallback("block"))

	err := http.ListenAndServe(":12345", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err.Error())
	}
}

const html = `
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN"
	"http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">

<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="en" lang="en">
<head>
	<meta http-equiv="Content-Type" content="text/html; charset=utf-8"/>
	<title>n6bagent</title>
</head>

<body>
    <h1><a href="http://github.com/tiancaiamao/n6bagent">n6bagent</a></h1>

      Welcome to use n6bagent!
</body>
</html>
`
