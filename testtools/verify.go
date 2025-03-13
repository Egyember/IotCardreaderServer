package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
)

var (
	key    = flag.String("k", "asd", "api key")
	token  = flag.String("t", "asd", "auth token")
	serial = flag.String("s", "asd", "serial number")
)

type verifyRequest struct {
	ApiKey       string `json:"apikey"`
	Authtoken    string `json:"authtoken"`
	SerialNumber string `json:"serialnumber"`
}

func main() {
	flag.Parse()
	ans := verifyRequest{
		ApiKey:       *key,
		Authtoken:    *token,
		SerialNumber: *serial,
	}
	js, _ := json.Marshal(ans)
	fmt.Println(string(js))
	r, _ := http.DefaultClient.Post("http://localhost:8090/api/request/verify", "application/json", bytes.NewReader(js))
	body, _ := io.ReadAll(r.Body)
	fmt.Println(len(body))
	fmt.Println(string(body))
}
