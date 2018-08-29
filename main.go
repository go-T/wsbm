package main

import (
	"bufio"
	"bytes"
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
	flagRequest     = flag.Uint("n", 0, "Total request")
	flagConcurrency = flag.Uint("c", 1, "Concurrency")
	flagQueries     = flag.String("q", "", "Text file contans url query per line")
	flagDryRun      = flag.Bool("dryrun", false, "Dryrun")
	flagOutput      = flag.String("o", "-", "Output file, '-':stdout, '':null, 'filepath':'filepath.out'")
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
		return os.Create(fmt.Sprintf("%s.%d", *flagOutput, id))
	}
}

func loadQueries() ([]string, error) {
	if *flagQueries == "" {
		return nil, nil
	}

	file, err := os.Open(*flagQueries)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	reader := bufio.NewReader(file)
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			break
		}
		lines = append(lines, string(bytes.TrimSpace(line)))
	}
	return lines, nil
}

type WsBenchmark struct {
	url     string
	queries []string
}

func NewWsBenchmark(url string, queries []string) *WsBenchmark {
	return &WsBenchmark{
		url:     url,
		queries: queries,
	}
}

func (b *WsBenchmark) Run(request, concurrency int) {
	var wg sync.WaitGroup
	wg.Add(concurrency)

	var count int32
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for {
				id := int(atomic.AddInt32(&count, 1))
				if id > request {
					return
				}

				if err := b.runTask(id); err != nil {
					logf("run task %d err:%s", id, err)
				} else {
					logf("run task %d OK", id)
				}
			}
		}()
	}
	wg.Wait()
}

func (b *WsBenchmark) DryRun(request, concurrency int) {
	for id := 1; id <= request; id++ {
		url, err := b.getUrl(id)
		if err != nil {
			logf("get url %d err:%s", id, err)
			continue
		}
		logf("+ %d %s", id, url.String())
	}
}

func (b *WsBenchmark) getUrl(id int) (*url.URL, error) {
	rawUrl := strings.Replace(b.url, "<id>", fmt.Sprint(id), -1)

	u, err := url.Parse(rawUrl)
	if err != nil {
		return nil, fmt.Errorf("parse url %s err:%s", rawUrl, err.Error())
	}

	if len(b.queries) > 0 {
		rawQuery := b.queries[id%len(b.queries)]
		newQuery, err := url.ParseQuery(rawQuery)
		if err != nil {
			return nil, fmt.Errorf("parse query %s err:%s", rawQuery, err.Error())
		}

		query := u.Query()
		for name, value := range newQuery {
			query[name] = value
		}
		u.RawQuery = query.Encode()
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
	}

	return u, nil
}

func (b *WsBenchmark) runTask(id int) error {
	url, err := b.getUrl(id)
	if err != nil {
		return err
	}

	output, err := openOutput(id)
	if err != nil {
		return err
	}
	defer output.Close()

	logf("+ %d %s", id, url.String())

	h := http.Header{"Origin": {"http://" + url.Host}}
	conn, _, err := websocket.DefaultDialer.Dial(url.String(), h)
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

	if flag.Arg(0) == "" {
		flag.Usage()
		os.Exit(1)
	}

	queries, err := loadQueries()
	if err != nil {
		panic(err)
	}

	request := int(*flagRequest)
	concurrency := int(*flagConcurrency)
	if request < 1 && len(queries) > 0 {
		request = len(queries)
	}
	if request < concurrency {
		request = concurrency
	}
	logf("request: %d, concurrency:%d", request, concurrency)

	bm := NewWsBenchmark(flag.Arg(0), queries)
	if *flagDryRun {
		bm.DryRun(request, concurrency)
	} else {
		bm.Run(request, concurrency)
	}
}
