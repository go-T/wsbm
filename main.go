package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

var (
	flagRequest     = flag.Uint("n", 1, "Request")
	flagConcurrency = flag.Uint("c", 1, "Concurrency")
	flagOutput      = flag.String("out", "", "Output file")

	newLine = []byte("\n")
)

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

func openOutput(id int) (io.WriteCloser, error) {
	switch *flagOutput {
	case "":
		return nopCloser{ioutil.Discard}, nil
	case "-":
		return nopCloser{os.Stdout}, nil
	default:
		return os.Create(fmt.Sprintf("%s.%d", *flagOutput, id))
	}
}

func worker(ctx context.Context, u *url.URL, id int) error {
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
		msgType, r, err := conn.NextReader()
		if err != nil {
			return err
		}
		switch msgType {
		case websocket.TextMessage:
		case websocket.BinaryMessage:
			io.Copy(output, r)
			output.Write(newLine)
		}
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [opts] connect \nopts:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	u, err := url.Parse(flag.Arg(0))
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

	req, con := int(*flagRequest), int(*flagConcurrency)
	if con > req {
		con = req
	}
	log.Println("n:", req, "c:", con)

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

				log.Println("+", id, u.String())
				if err = worker(nil, u, id); err != nil {
					log.Println("-", id, err)
				}
			}
		}()
	}
	wg.Wait()
}
