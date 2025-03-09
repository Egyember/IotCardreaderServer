package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"net/http"
	"os"
	"sync"
	"text/template"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*
var embedFs embed.FS

var (
	database  *sql.DB
	htmltmpl  *htmltemplate.Template
	txttmpl   *template.Template
	authstore autstore
)

var (
	dbpath   = flag.String("dbpath", "./database.db", "path to the db file")
	addUser  = flag.Bool("add", false, "add admin to database")
	adminTab = flag.Bool("A", false, "add admin with adminTab permission")
	username = flag.String("u", "", "username when adding user to db")
	password = flag.String("p", "", "username when adding user to db")
)

var ErrInvalidCooki = errors.New("invalid auth cookie")

type (
	contextkey string
	headerdata struct {
		Uname    string
		Loggedin bool
		AdminTab bool
		Title    string
	}
	card struct {
		SerialNumber string
		WriteKey     string
		ReadKey      string
		Owner        int
	}
	cardReader struct {
		Id        int
		ApiKey    string
		AddCard   bool
		WriteCard bool
	}
	people struct {
		id         int
		authtoken  string
		name       string
		permission string
	}
	renderData struct {
		Status headerdata
		Filds  []map[string]any
	}
	authcookie struct {
		cookie   string
		uname    string
		time     time.Time
		adminTab bool
	}
	autstore struct {
		cookies []authcookie
		lock    sync.Mutex
		ticker  time.Ticker
		done    chan bool
	}
)

func (s *autstore) valid(cookie string) (string, bool, error) {
	s.lock.Lock()
	for k, v := range s.cookies {
		if (v.cookie == cookie) && (time.Since(v.time) < 1*time.Hour) {
			s.cookies[k].time = time.Now()
			uname := v.uname
			ad := v.adminTab
			s.lock.Unlock()
			return uname, ad, nil
		}
	}
	s.lock.Unlock()
	return "", false, ErrInvalidCooki
}

func (s *autstore) add(cookie, uname string, admintab bool) {
	now := time.Now()
	c := authcookie{cookie: cookie, uname: uname, time: now, adminTab: admintab}
	s.lock.Lock()
	s.cookies = append(s.cookies, c)
	s.lock.Unlock()
}

func (s *autstore) Clean() {
	for {
		select {
		case <-s.done:
			return
		case <-s.ticker.C:
			s.lock.Lock()
			newcookies := make([]authcookie, 0, len(s.cookies))
			for _, v := range s.cookies {
				if time.Since(v.time) < 1*time.Hour {
					newcookies = append(newcookies, v)
				}
			}
			s.cookies = newcookies
			s.lock.Unlock()
		}
	}
}

func rootHandler(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, "./admin", http.StatusSeeOther)
}

func loginNeeded(next http.Handler, admintab bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("AUTH")
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}
		}
		uname, at, err := authstore.valid(c.Value)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		if admintab {
			if !at {
				http.Error(w, "Access Denied", http.StatusForbidden)
				return
			}
		}
		cont := r.Context()
		cont = context.WithValue(cont, contextkey("uname"), uname)
		cont = context.WithValue(cont, contextkey("adminTab"), at)
		next.ServeHTTP(w, r.WithContext(cont))
		return
	})
}

func computepwHash(pw []byte) []byte {
	hash, _ := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost)
	return hash
}

func login(w http.ResponseWriter, r *http.Request) {
	drawLogin := func(Failed bool) {
		status := headerdata{Loggedin: false, Title: "login", AdminTab: false}
		err := htmltmpl.ExecuteTemplate(w, "login.html", struct {
			Status headerdata
			Failed bool
		}{Status: status, Failed: Failed})
		if err != nil {
			fmt.Println(err)
		}
	}
	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			fmt.Println("error: ", err.Error())
			return
		}
		uname := r.FormValue("username")
		passwd := r.FormValue("password")
		tx, err := database.Begin()
		if err != nil {
			fmt.Println("error: ", err.Error())
			return
		}
		row := tx.QueryRow("SELECT pwhash, adminTab FROM admins WHERE username=? LIMIT 1", uname)
		var dbHash string
		var adminTab bool
		err = row.Scan(&dbHash, &adminTab)
		if err != nil {
			fmt.Println("error: ", err.Error())
			drawLogin(true)
			return
		}
		err = bcrypt.CompareHashAndPassword([]byte(dbHash), []byte(passwd))
		if err != nil {
			fmt.Println(err)
			drawLogin(true)
		}
		authtoken := crand.Text()
		authstore.add(authtoken, uname, adminTab.(bool))
		cookie := http.Cookie{
			Name:     "AUTH",
			Value:    authtoken,
			Path:     "/",
			MaxAge:   3600,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, &cookie)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	} else {
		drawLogin(false)
	}
}

func admin(w http.ResponseWriter, r *http.Request) {
	cont := r.Context()
	uname := cont.Value(contextkey("uname")).(string)
	admintab := cont.Value(contextkey("uname")).(bool)
	status := headerdata{Loggedin: true, Title: "main", Uname: uname, AdminTab: admintab}
	fmt.Println("render main")
	err := htmltmpl.ExecuteTemplate(w, "home.html", status)
	if err != nil {
		fmt.Println(err)
	}
}

func tableFactory(title string, fildNames []string, table string) http.HandlerFunc {
	sqlfilds := ""
	for _, v := range fildNames {
		sqlfilds += v
		sqlfilds += ", "
	}
	sqlfilds = sqlfilds[0:(len(sqlfilds) - 2)]
	query := fmt.Sprintf("Select %s from %s", sqlfilds, table)
	args := struct {
		FildNames []string
		Url       string
	}{fildNames, title}
	buff := new(bytes.Buffer)
	txttmpl.ExecuteTemplate(buff, "magic.html.tmpl", args)
	templ, err := htmltmpl.Clone()
	if err != nil {
		panic(err)
	}
	templ, err = templ.Parse(buff.String())
	if err != nil {
		fmt.Println("fuck this shit")
		panic(err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		cont := r.Context()
		uname := cont.Value(contextkey("uname")).(string)
		admintab := cont.Value(contextkey("uname")).(bool)
		status := headerdata{Loggedin: true, Title: title, Uname: uname, AdminTab: admintab}
		tx, err := database.Begin()
		if err != nil {
			fmt.Println(err)
			return
		}
		rows, err := tx.Query(query)
		if err != nil {
			fmt.Println(err)
			tx.Rollback()
			return
		}
		defer rows.Close()

		var data renderData
		data.Status = status
		data.Filds = make([]map[string]any, 0)
		cols, _ := rows.Columns()
		for rows.Next() {
			// Create a slice of interface{}'s to represent each column,
			// and a second slice to contain pointers to each item in the columns slice.
			columns := make([]interface{}, len(cols))
			columnPointers := make([]interface{}, len(cols))
			for i := range columns {
				columnPointers[i] = &columns[i]
			}

			err := rows.Scan(columnPointers...)
			if err != nil {
				fmt.Println("error: " + err.Error())
				tx.Rollback()
				return
			}
			m := make(map[string]interface{})
			for i, colName := range cols {
				val := columnPointers[i].(*interface{})
				m[colName] = *val
			}
			data.Filds = append(data.Filds, m)
		}
		tx.Commit()
		err = templ.ExecuteTemplate(w, "magic", data)
		if err != nil {
			fmt.Println(err)
		}
	}
}
func addFactory() {}

func main() {
	flag.Parse()
	// init templates

	htmlfs, err := fs.Sub(embedFs, "templates/htmltemplates")
	if err != nil {
		panic(err)
	}
	htmltmpl = htmltemplate.Must(htmltmpl.ParseFS(htmlfs, "*html"))
	txtfs, err := fs.Sub(embedFs, "templates/txttemplates")
	if err != nil {
		panic(err)
	}
	txttmpl = template.Must(template.ParseFS(txtfs, "*tmpl"))
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
		_, err := tx.Exec("INSERT INTO admins (username, pwhash, adminTab) VALUES (?, ?, ?)", *username, computepwHash([]byte(*password)), *adminTab)
		if err != nil {
			fmt.Println("failed: ", err.Error())
			tx.Rollback()
		} else {
			tx.Commit()
		}
		return
	}

	// cooki store init
	authstore.cookies = make([]authcookie, 0)
	authstore.ticker = *time.NewTicker(1 * time.Hour)
	authstore.done = make(chan bool)
	go authstore.Clean()

	http.HandleFunc("GET /{$}", rootHandler)
	http.Handle("/admin", loginNeeded(http.HandlerFunc(admin), false))
	cardsHandler := tableFactory("cards", []string{"serialNumber", "writeKey", "readKey", "owner"}, "cards")
	http.Handle("/admin/cards", loginNeeded(http.HandlerFunc(cardsHandler), false))
	readerHandler := tableFactory("readers", []string{"id", "apiKey", "addCard", "writeCard"}, "reader")
	http.Handle("/admin/readers", loginNeeded(http.HandlerFunc(readerHandler), false))
	peopleHandler := tableFactory("people", []string{"id", "authtoken", "name", "permission"}, "people")
	http.Handle("/admin/people", loginNeeded(http.HandlerFunc(peopleHandler), false))
	logHandler := tableFactory("logs", []string{"id", "card", "reader", "people", "allowed", "direction", "comment"}, "accessLog")
	http.Handle("/admin/logs", loginNeeded(http.HandlerFunc(logHandler), false))
	adminsHandler := tableFactory("logs", []string{"id", "username", "pwhash", "adminTab"}, "admins")
	http.Handle("/admin/admins", loginNeeded(http.HandlerFunc(adminsHandler), true))
	http.HandleFunc("/admin/login", login)
	http.ListenAndServe(":8090", nil)

	authstore.done <- true
}
