package n6bagent

import (
    "bufio"
    "bytes"
    "code.google.com/p/go.net/websocket"
    "encoding/binary"
    "io"
    "log"
    "net/http"
    "net/url"
    // "os"
    "runtime/pprof"
    // "time"
)

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

type Server struct {
    *http.ServeMux
}

func NewServer() *Server {
    mux := http.NewServeMux()
    mux.HandleFunc("/", indexCallback)
    mux.Handle("/websocket", websocket.Handler(websocketCallback))
    mux.HandleFunc("/pprof/goroutine", debugCallback("goroutine"))
    mux.HandleFunc("/pprof/heap", debugCallback("heap"))
    mux.HandleFunc("/pprof/threadcreate", debugCallback("threadcreate"))
    mux.HandleFunc("/pprof/block", debugCallback("block"))
    return &Server{mux}
}

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

func worker(session uint32, r *http.Request, w chan<- []byte) {
    log.Printf("SESSION %d BEGIN: %s %s\n", session, r.Method, r.URL.String())

    resp, err := http.DefaultClient.Do(r)
    if err != nil {
        log.Println("client.Do error:", err)
        return
    }
    defer resp.Body.Close()

    buf := &bytes.Buffer{}
    binary.Write(buf, binary.LittleEndian, session)
    binary.Write(buf, binary.LittleEndian, uint32(0))

    resp.Write(buf)

    log.Printf("SESSION %d END size=%d\n", session, buf.Len()-8)

    data := buf.Bytes()
    binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))

    w <- data
}

func websocketCallback(ws *websocket.Conn) {
    ch := make(chan []byte)
    go func(c <-chan []byte) {
        for {
            data := <-c
            ws.Write(data)
        }
    }(ch)

    buf := &bytes.Buffer{}

    var session uint32
    var size uint32

    for {
        err := binary.Read(ws, binary.LittleEndian, &session)
        if err != nil {
            ws.Close()
            return
        }
        err = binary.Read(ws, binary.LittleEndian, &size)
        if err != nil {
            ws.Close()
            return
        }

        buf.Reset()
        _, err = io.CopyN(buf, ws, int64(size))
        if err != nil {
            log.Println("read websocket error:", session, err)
            continue
        }

        bufreader := bufio.NewReader(buf)
        req, err := http.ReadRequest(bufreader)
        if err != nil {
            log.Println("read request error:", session, err)
            continue
        }
        req.URL, err = url.Parse("http://" + req.Host + req.URL.String())
        req.RequestURI = ""

        go worker(session, req, ch)
    }
}