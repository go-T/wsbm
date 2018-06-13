package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

var (
	flagRequest     = flag.Uint("n", 1, "Total request")
	flagConcurrency = flag.Uint("c", 1, "Concurrency")
	flagOutput      = flag.String("o", "-", "Output file, '-':stdout, '':null, 'filepath':'filepath-<id>.out'")
)

func logf(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", v...)
}

type discard struct{}

func (d discard) Close() error                      { return nil }
func (d discard) Write(p []byte) (n int, err error) { return len(p), nil }

type stdout struct{}

func (s stdout) Close() error { return nil }
func (s stdout) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

func openOutput(id int) (io.WriteCloser, error) {
	switch *flagOutput {
	case "":
		return discard{}, nil
	case "-":
		return stdout{}, nil
	default:
		return os.Create(fmt.Sprintf("%s-%d.out", *flagOutput, id))
	}
}

func worker(u *url.URL, id int) error {
	output, err := openOutput(id)
	if err != nil {
		return err
	}
	defer output.Close()

	h := http.Header{"Origin": {"http://" + u.Host}}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), h)
	if err != nil {
		return err
	}
	defer conn.Close()

	for {
		msgType, content, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		switch msgType {
		case websocket.TextMessage, websocket.BinaryMessage:
			output.Write(content)
		}
	}
}

func main() {
	flag.Usage = func() {
		const usage = `Usage: wsbm [options] <url>
    '<id>' in url will be replace by connection id
options:`
		fmt.Fprintln(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	req, con := int(*flagRequest), int(*flagConcurrency)
	if con > req {
		req = con
	}
	logf("request: %d, concurrency:%d", req, con)

	var wg sync.WaitGroup
	wg.Add(con)

	var count int32
	for i := 0; i < con; i++ {
		go func() {
			defer wg.Done()
			for {
				id := int(atomic.AddInt32(&count, 1))
				if id > req {
					return
				}

				u, err := url.Parse(strings.Replace(flag.Arg(0), "<id>", fmt.Sprint(id), -1))
				if err != nil {
					panic(err)
				}

				switch u.Scheme {
				case "http":
					u.Scheme = "ws"
				case "https":
					u.Scheme = "wss"
				default:
				}

				logf("+ %d %s", id, u.String())
				if err = worker(u, id); err != nil {
					logf("- %d %s", id, err)
				}
			}
		}()
	}
	wg.Wait()
}
