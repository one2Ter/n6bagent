package n6bagent

import (
    "bufio"
    "bytes"
    "code.google.com/p/go.net/websocket"
    "encoding/binary"
    "errors"
    // "fmt"
    "io"
    "io/ioutil"
    "log"
    "net/http"
    // "os"
    "sync"
    "sync/atomic"
)

type Cell struct {
    w   http.ResponseWriter
    r   *http.Request
    c   chan struct{}
}
type Result struct {
    sync.Mutex
    kv  map[uint32]*Cell
}

func newResult() *Result {
    return &Result{
        kv: make(map[uint32]*Cell),
    }
}

func (r *Result) Put(session uint32, w *Cell) {
    r.Lock()
    r.kv[session] = w
    r.Unlock()
}

func (r *Result) Del(session uint32) (*Cell, error) {
    r.Lock()
    ret, ok := r.kv[session]
    if !ok {
        r.Unlock()
        return nil, errors.New("not exist")
    }
    r.Unlock()
    return ret, nil
}

func sender(ws *websocket.Conn, ch <-chan []byte) {
    for {
        buf := <-ch

        wn := 0
        total := len(buf)
        for wn != total {
            n, err := ws.Write(buf[wn:total])
            if err != nil {
                log.Println("handleConn Write error: ", err)
                return
            }
            wn += n
        }
    }
}

func readSession(ws *websocket.Conn, buf *bytes.Buffer) (uint32, error) {
    var session uint32
    var size uint32

    err := binary.Read(ws, binary.LittleEndian, &session)
    if err != nil {
        return 0, err
    }
    binary.Read(ws, binary.LittleEndian, &size)
    if err != nil {
        return 0, err
    }

    buf.Reset()
    log.Printf("session %d size = %d\n", session, size)
    _, err = io.CopyN(buf, ws, int64(size))
    if err != nil {
        return 0, err
    }
    return session, nil
}

func receiver(ws *websocket.Conn, res *Result) {
    buf := &bytes.Buffer{}
    for {
        session, err := readSession(ws, buf)
        if err != nil {
            log.Fatal("readSession error: ", err)
        }

        c, err := res.Del(session)
        if err != nil {
            log.Printf("session %d not exist!!!", session)
            continue
        }

        bufreader := bufio.NewReader(buf)
        resp, err := http.ReadResponse(bufreader, c.r)
        if err != nil {
            log.Printf("read response error!!!!")
            c.c <- struct{}{}
            continue
        }

        for k, v := range resp.Header {
            for _, vv := range v {
                c.w.Header().Add(k, vv)
            }
        }

        result, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            log.Println("读Body也可以出错?", err)
            c.c <- struct{}{}
            continue
        }

        c.w.Write(result)
        if err != nil {
            log.Println("写回resp错误：", err)
        }
        c.c <- struct{}{}
    }
}

var ()

type Client struct {
    session uint32
    Ch      chan []byte
    Res     *Result
}

func NewClient(hostAddr string) (*Client, error) {
    ch := make(chan []byte)
    res := newResult()

    origin := "http://" + hostAddr + "/"
    url := "ws://" + hostAddr + "/websocket"

    ws, err := websocket.Dial(url, "", origin)
    if err != nil {
        return nil, err
    }

    go sender(ws, ch)
    go receiver(ws, res)
    return &Client{0, ch, res}, nil
}

func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    sessionID := atomic.AddUint32(&c.session, 1)
    log.Printf("SESSION %d BEGIN: %s %s\n", sessionID, r.Method, r.URL.String())

    buf := &bytes.Buffer{}
    binary.Write(buf, binary.LittleEndian, sessionID)
    binary.Write(buf, binary.LittleEndian, uint32(0))
    r.Write(buf)

    b := buf.Bytes()
    binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))

    wait := make(chan struct{})
    // 等待返回结束
    c.Res.Put(sessionID, &Cell{
        w, r, wait,
    })

    c.Ch <- b
    <-wait
    log.Printf("SESSION %d END\n", sessionID)
}