package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <URL>", os.Args[0])
	}
	u := must(url.Parse(os.Args[1]))
	secret := strings.TrimSpace(os.Getenv("COOKIE"))
	req := must(http.NewRequest(
		"POST",
		"http://127.0.0.1:"+u.Query()["callback"][0],
		strings.NewReader(fmt.Sprintf(`{"secret": "%s"}`, secret))))
	must(http.DefaultClient.Do(req))
}

func must[T any](x T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return x
}
