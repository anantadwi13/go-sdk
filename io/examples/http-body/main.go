package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkIo "github.com/anantadwi13/go-sdk/io"
)

/*
	Output

	2024/10/09 16:25:48 server listen on 127.0.0.1:34395
	2024/10/09 16:25:49 server first read abcde
	2024/10/09 16:25:49 server second read abcdefghij
	2024/10/09 16:25:49 server third read abcdefghij1234567890
	2024/10/09 16:25:49 server has disabled seeker. true
*/

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	defer wg.Wait()

	listener := listenTCP()

	wg.Add(1)
	go func() {
		defer wg.Done()
		server(ctx, listener)
	}()

	client(listener)
	cancel()
}

func client(listener net.Listener) {
	time.Sleep(1 * time.Second)

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", listener.Addr().String()), strings.NewReader("abcdefghij1234567890"))
	if err != nil {
		panic(err)
	}

	_, err = http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
}

func server(ctx context.Context, listener net.Listener) {
	brscFactory := sdkIo.NewBufferReadSeekCloserFactory()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body := brscFactory.NewReader(r.Body)

		defer func() {
			_, _ = io.Copy(sdkIo.Discard, body) // drain body
			_ = body.Close()
		}()

		buf := &bytes.Buffer{}
		_, err := io.CopyN(buf, body, 5)
		if err != nil {
			panic(err)
		}

		log.Println("server first read", buf.String())

		_, err = body.Seek(0, io.SeekStart)
		if err != nil {
			panic(err)
		}

		buf.Reset()

		_, err = io.CopyN(buf, body, 10)
		if err != nil {
			panic(err)
		}

		log.Println("server second read", buf.String())

		_, err = body.Seek(0, io.SeekStart)
		if err != nil {
			panic(err)
		}
		body.DisableSeeker()

		buf.Reset()

		_, err = io.Copy(buf, body)
		if err != nil {
			panic(err)
		}

		log.Println("server third read", buf.String())

		_, err = body.Seek(0, io.SeekStart)
		if err != nil {
			log.Println("server has disabled seeker.", errors.Is(err, sdkIo.ErrSeekerDisabled))
		}

		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{}

	wg := sync.WaitGroup{}
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()

		log.Printf("server listen on %s", listener.Addr().String())
		err := srv.Serve(listener)
		if err != nil {
			return
		}
	}()

	<-ctx.Done()
	_ = srv.Shutdown(context.Background())
}

func listenTCP() net.Listener {
	listen, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	return listen
}
