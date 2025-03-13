package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"text/template"
	"time"

	"server/frontend"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*
var embedFs embed.FS

var (
	database *sql.DB
	htmltmpl *htmltemplate.Template
	txttmpl  *template.Template
)

var (
	dbpath   = flag.String("dbpath", "./database.db", "path to the db file")
	addUser  = flag.Bool("add", false, "add admin to database")
	adminTab = flag.Bool("A", false, "add admin with adminTab permission")
	username = flag.String("u", "", "username when adding user to db")
	password = flag.String("p", "", "username when adding user to db")
)

type (
	card struct {
		SerialNumber string `json:"serialnumber"`
		Authtoken    string `json:"authtoken"`
		WriteKey     string `json:"writekey"`
		ReadKey      string `json:"readkey"`
		Owner        int    `json:"owner"`
	}
	cardReader struct {
		Id        int
		ApiKey    string
		AddCard   bool
		WriteCard bool
	}
	people struct {
		Id         int    `json:"ok"`
		Name       string `json:"name"`
		Permission string `json:"perm"`
	}
	keyRequest struct {
		ApiKey       string `json:"apikey"`
		SerialNumber string `json:"serialnumber"`
		Write        bool   `json:"write"`
	}
	keyAns struct {
		Ok  bool   `json:"ok"`
		Key string `json:"key"`
	}
	verifyRequest struct {
		ApiKey       string `json:"apikey"`
		Authtoken    string `json:"authtoken"`
		SerialNumber string `json:"serialnumber"`
	}
	verifyAns struct {
		Ok         bool   `json:"ok"`
		Name       string `json:"name"`
		Permission string `json:"perm"`
	}
	addCardRequest struct {
		ApiKey       string `json:"apikey"`
		SerialNumber string `json:"serialnumber"`
	}
	addCardAns struct {
		Ok        bool   `json:"ok"`
		Authtoken string `json:"authtoken"`
		WriteKey  string `json:"writekey"`
		ReadKey   string `json:"readkey"`
	}
)

func addLog(card, reader, people, allowed, direction, comment any) {
	tx, err := database.Begin()
	defer tx.Commit()
	if err != nil {
		panic(err)
	}
	_, err = tx.Exec("INSERT INTO accessLog (card, reader, people, allowed, direction, comment) VALUES (?, ?, ?, ?, ?, ?)", card, reader, people, allowed, direction, comment)
	if err != nil {
		panic(err)
	}
}

func verifyRequestHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var request verifyRequest
	err = json.Unmarshal(body, &request)
	if err != nil {
		ans := verifyAns{
			Ok:         false,
			Name:       "",
			Permission: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		return
	}
	tx, err := database.Begin()
	if err != nil {
		panic(err)
	}
	row := tx.QueryRow("SELECT id FROM reader WHERE apiKey = ?", request.ApiKey)
	var readerId int
	err = row.Scan(&readerId)
	if err != nil {
		tx.Rollback()
		fmt.Println(err.Error())
		ans := verifyAns{
			Ok:         false,
			Name:       "",
			Permission: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("bad api key")
		addLog(request.SerialNumber, nil, nil, false, nil, nil)
		return
	}
	row = tx.QueryRow("SELECT people.id, name, permission FROM cards INNER JOIN people ON cards.owner = people.id WHERE cards.authtoken = ? and cards.serialNumber = ?", request.Authtoken, request.SerialNumber)
	// INNER JOIN people ON cards.owner = people.id
	peopleId := 0
	Name := ""
	Perm := ""
	err = row.Scan(&peopleId, &Name, &Perm)
	if err != nil {
		tx.Rollback()
		ans := verifyAns{
			Ok:         false,
			Name:       "",
			Permission: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("bad serial number & auth key")
		addLog(request.SerialNumber, readerId, nil, false, nil, nil)
		return
	}
	ans := verifyAns{
		Ok:         true,
		Name:       Name,
		Permission: Perm,
	}
	js, err := json.Marshal(ans)
	if err != nil {
		panic(err)
	}
	w.Write(js)
	tx.Rollback()
	addLog(request.SerialNumber, readerId, peopleId, true, nil, nil)
}

func keyRequestHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var request keyRequest
	err = json.Unmarshal(body, &request)
	if err != nil {
		ans := keyAns{
			Ok:  false,
			Key: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		return
	}
	tx, err := database.Begin()
	if err != nil {
		panic(err)
	}
	row := tx.QueryRow("SELECT id, writeCard FROM reader WHERE apiKey = ?", request.ApiKey)
	var reader cardReader
	err = row.Scan(&reader.Id, &reader.WriteCard)
	if err != nil {
		tx.Rollback()
		fmt.Println(err.Error())
		ans := keyAns{
			Ok:  false,
			Key: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("bad api key")
		addLog(nil, nil, nil, false, nil, "key request denied wrong api key")
		return
	}
	row = tx.QueryRow("SELECT writeKey, readKey permission FROM cards WHERE serialNumber = ?", request.SerialNumber)
	var readKey string
	var writeKey string
	err = row.Scan(&writeKey, &readKey)
	if err != nil {
		tx.Rollback()
		fmt.Println(err.Error())
		ans := keyAns{
			Ok:  false,
			Key: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("bad api key")
		addLog(request.SerialNumber, reader.Id, nil, false, nil, "scan failed")
		return
	}
	ans := keyAns{
		Ok:  true,
		Key: "",
	}
	if request.Write {
		if reader.WriteCard {
			ans.Key = writeKey
		} else {
			ans.Ok = false
			ans.Key = ""
		}
	} else {
		ans.Key = readKey
	}
	js, err := json.Marshal(ans)
	if err != nil {
		panic(err)
	}
	w.Write(js)
	tx.Rollback()
	addLog(request.SerialNumber, reader.Id, nil, ans.Ok, nil, fmt.Sprintf("writekey value was: %v", request.Write))
}

func addCardRequestHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	var request addCardRequest
	err = json.Unmarshal(body, &request)
	if err != nil {
		ans := addCardAns{
			Ok:        false,
			ReadKey:   "",
			WriteKey:  "",
			Authtoken: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		return
	}
	tx, err := database.Begin()
	if err != nil {
		panic(err)
	}
	row := tx.QueryRow("SELECT id, addCard FROM reader WHERE apiKey = ?", request.ApiKey)
	var reader cardReader
	err = row.Scan(&reader.Id, &reader.AddCard)
	if err != nil {
		tx.Rollback()
		fmt.Println(err.Error())
		ans := addCardAns{
			Ok:        false,
			ReadKey:   "",
			WriteKey:  "",
			Authtoken: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("bad api key")
		addLog(nil, nil, nil, false, nil, "addcard request denied wrong api key")
		return
	}
	if !reader.AddCard {
		tx.Rollback()
		ans := addCardAns{
			Ok:        false,
			ReadKey:   "",
			WriteKey:  "",
			Authtoken: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("permission denied")
		addLog(nil, reader.Id, nil, false, nil, "card add permission denied")
		return
	}
	readKey := make([]byte, 6)
	writeKey := make([]byte, 6)
	authtok := make([]byte, 16)
	_, err = rand.Read(readKey)
	if err != nil {
		panic(err)
	}
	_, err = rand.Read(writeKey)
	if err != nil {
		panic(err)
	}
	_, err = rand.Read(authtok)
	if err != nil {
		panic(err)
	}
	rkeybuff := bytes.NewBuffer(make([]byte, 0))
	rkencoder := base64.NewEncoder(base64.RawStdEncoding.Strict(), rkeybuff)
	_, err = rkencoder.Write(readKey)
	if err != nil {
		panic(err)
	}
	b64rkey, err := io.ReadAll(rkeybuff)
	if err != nil {
		panic(err)
	}
	wkeybuff := bytes.NewBuffer(make([]byte, 0))
	wkencoder := base64.NewEncoder(base64.RawStdEncoding.Strict(), wkeybuff)
	_, err = wkencoder.Write(writeKey)
	if err != nil {
		panic(err)
	}
	b64wkey, err := io.ReadAll(wkeybuff)
	if err != nil {
		panic(err)
	}
	authbuff := bytes.NewBuffer(make([]byte, 0))
	authencoder := base64.NewEncoder(base64.RawStdEncoding.Strict(), authbuff)
	_, err = authencoder.Write(readKey)
	if err != nil {
		panic(err)
	}
	b64auth, err := io.ReadAll(authbuff)
	if err != nil {
		panic(err)
	}
	ans := addCardAns{
		Ok:        true,
		ReadKey:   string(b64rkey),
		WriteKey:  string(b64wkey),
		Authtoken: string(b64auth),
	}
	_, err = tx.Exec("INSERT INTO cards (serialNumber, authtoken, writeKey, readKey, owner) VALUES (?, ?, ?, ?, 0)", request.SerialNumber, ans.Authtoken, ans.WriteKey, ans.ReadKey)
	if err != nil {
		fmt.Println(err.Error())
		tx.Rollback()
		ans := addCardAns{
			Ok:        false,
			ReadKey:   "",
			WriteKey:  "",
			Authtoken: "",
		}
		js, err := json.Marshal(ans)
		if err != nil {
			panic(err)
		}
		w.Write(js)
		fmt.Println("failed to add card to db")
		addLog(nil, reader.Id, nil, false, nil, "failed to add card")
		return
	}
	js, err := json.Marshal(ans)
	if err != nil {
		panic(err)
	}
	w.Write(js)
	tx.Commit()
	addLog(request.SerialNumber, reader.Id, 0, ans.Ok, nil, "added card")
}

func jsonAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-type") != "application/json" {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	flag.Parse()
	// init templates

	htmlfs, err := fs.Sub(embedFs, "templates/htmltemplates")
	if err != nil {
		panic(err)
	}
	frontend.Htmltmpl = htmltemplate.Must(htmltmpl.ParseFS(htmlfs, "*html"))
	txtfs, err := fs.Sub(embedFs, "templates/txttemplates")
	if err != nil {
		panic(err)
	}
	txttmpl = template.Must(template.ParseFS(txtfs, "*tmpl"))
	frontend.Txttmpl = txttmpl
	// init a new db if file dosn't exist
	if _, err := os.Stat(*dbpath); errors.Is(err, os.ErrNotExist) {
		fd, err := os.Create(*dbpath)
		if err != nil {
			panic(err)
		}
		fd.Close()
		database, err := sql.Open("sqlite3", *dbpath)
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		tx, err := database.Begin()
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		create := new(bytes.Buffer)
		err = txttmpl.ExecuteTemplate(create, "create.sql.tmpl", nil)
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		_, err = tx.Exec(create.String())
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		err = tx.Commit()
		if err != nil {
			os.Remove(*dbpath)
			panic(err)
		}
		database.Close()
	}
	// open db connection
	database, err = sql.Open("sqlite3", *dbpath)
	if err != nil {
		panic(err)
	}
	defer database.Close()
	if *addUser {
		tx, _ := database.Begin()
		_, err := tx.Exec("INSERT INTO admins (username, pwhash, adminTab) VALUES (?, ?, ?)", *username, frontend.ComputepwHash([]byte(*password)), *adminTab)
		if err != nil {
			fmt.Println("failed: ", err.Error())
			tx.Rollback()
		} else {
			tx.Commit()
		}
		return
	}

	frontend.Database = database

	// frontend cooki store init
	frontend.Authstore.Cookies = make([]frontend.Authcookie, 0)
	frontend.Authstore.Ticker = *time.NewTicker(1 * time.Hour)
	frontend.Authstore.Done = make(chan bool)
	go frontend.Authstore.Clean()
	frontend.AddEndpoints()

	http.Handle("POST /api/request/verify", jsonAPI(http.HandlerFunc(verifyRequestHandler)))
	http.Handle("POST /api/request/key", jsonAPI(http.HandlerFunc(keyRequestHandler)))
	http.Handle("POST /api/request/addCard", jsonAPI(http.HandlerFunc(addCardRequestHandler)))
	http.ListenAndServe(":8090", nil)

	frontend.Authstore.Done <- true
}
