package frontend

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"net/http"
	"strconv"
	"sync"
	"text/template"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)

var ErrInvalidCooki = errors.New("invalid auth cookie")

var (
	Database  *sql.DB
	Htmltmpl  *htmltemplate.Template
	Txttmpl   *template.Template
	Authstore autstore
)

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
	Authcookie struct {
		cookie   string
		uname    string
		time     time.Time
		adminTab bool
	}
	autstore struct {
		Cookies []Authcookie
		lock    sync.Mutex
		Ticker  time.Ticker
		Done    chan bool
	}
)

func (s *autstore) valid(cookie string) (string, bool, error) {
	s.lock.Lock()
	for k, v := range s.Cookies {
		if (v.cookie == cookie) && (time.Since(v.time) < 1*time.Hour) {
			s.Cookies[k].time = time.Now()
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
	c := Authcookie{cookie: cookie, uname: uname, time: now, adminTab: admintab}
	s.lock.Lock()
	s.Cookies = append(s.Cookies, c)
	s.lock.Unlock()
}

func (s *autstore) Clean() {
	for {
		select {
		case <-s.Done:
			return
		case <-s.Ticker.C:
			s.lock.Lock()
			newcookies := make([]Authcookie, 0, len(s.Cookies))
			for _, v := range s.Cookies {
				if time.Since(v.time) < 1*time.Hour {
					newcookies = append(newcookies, v)
				}
			}
			s.Cookies = newcookies
			s.lock.Unlock()
		}
	}
}

func RootHandler(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, "./admin", http.StatusSeeOther)
}

func LoginNeeded(next http.Handler, admintab bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("AUTH")
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}
		}
		uname, at, err := Authstore.valid(c.Value)
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
		c.MaxAge = 3600
		c.Path = "/"
		http.SetCookie(w, c)
		cont := r.Context()
		cont = context.WithValue(cont, contextkey("uname"), uname)
		cont = context.WithValue(cont, contextkey("adminTab"), at)
		next.ServeHTTP(w, r.WithContext(cont))
		return
	})
}

func ComputepwHash(pw []byte) []byte {
	hash, _ := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost)
	return hash
}

func Login(w http.ResponseWriter, r *http.Request) {
	drawLogin := func(Failed bool) {
		status := headerdata{Loggedin: false, Title: "login", AdminTab: false}
		err := Htmltmpl.ExecuteTemplate(w, "login.html", struct {
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
		tx, err := Database.Begin()
		defer tx.Rollback()
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
		Authstore.add(authtoken, uname, adminTab)
		cookie := http.Cookie{
			Name:     "AUTH",
			Value:    authtoken,
			Path:     "/",
			MaxAge:   3600,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		}
		http.SetCookie(w, &cookie)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	} else {
		drawLogin(false)
	}
}

func Logout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("AUTH")
	if err != nil {
		println(err)
		return
	}
	c.MaxAge = -1
	http.SetCookie(w, c)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func Admin(w http.ResponseWriter, r *http.Request) {
	cont := r.Context()
	uname := cont.Value(contextkey("uname")).(string)
	admintab := cont.Value(contextkey("adminTab")).(bool)
	status := headerdata{Loggedin: true, Title: "main", Uname: uname, AdminTab: admintab}
	fmt.Println("render main")
	err := Htmltmpl.ExecuteTemplate(w, "home.html", status)
	if err != nil {
		fmt.Println(err)
	}
}

func TableFactory(title string, fildNames []string, table string) http.HandlerFunc {
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
	Txttmpl.ExecuteTemplate(buff, "magic.html.tmpl", args)
	templ, err := Htmltmpl.Clone()
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
		admintab := cont.Value(contextkey("adminTab")).(bool)
		status := headerdata{Loggedin: true, Title: title, Uname: uname, AdminTab: admintab}
		tx, err := Database.Begin()
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

func AddFactory(title string, fildNames []string, fildTypes []string, table string) http.HandlerFunc {
	type FildNames struct {
		Name string
		Type string
	}
	fnames := make([]FildNames, len(fildNames))
	for k, v := range fildNames {
		fnames[k].Name = v
		fnames[k].Type = fildTypes[k]
	}
	args := struct {
		FildNames []FildNames
		Url       string
	}{fnames, title}
	buff := new(bytes.Buffer)
	Txttmpl.ExecuteTemplate(buff, "magicAdd.html.tmpl", args)
	templ, err := Htmltmpl.Clone()
	if err != nil {
		panic(err)
	}
	templ, err = templ.Parse(buff.String())
	if err != nil {
		panic(err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			r.ParseForm()
			queryfilds := make([]string, 0, len(fildNames))
			queryvalues := make([]any, 0, len(fildNames))
			for k, v := range fildNames {
				b := r.FormValue(v + "box")
				if b != "" {
					queryfilds = append(queryfilds, v)
					value := r.FormValue(v)
					if fildTypes[k] == "number" {
						n, err := strconv.Atoi(value)
						if err != nil {
							fmt.Println(err)
							return
						}
						queryvalues = append(queryvalues, n)
					} else {
						queryvalues = append(queryvalues, value)
					}
				}
			}
			query := "INSERT INTO " + table + " ("
			for _, v := range queryfilds {
				query += v + ", "
			}
			query = query[:len(query)-2] + ") VALUES ("
			for range queryvalues {
				query += "?, "
			}
			query = query[:len(query)-2] + ")"
			tx, err := Database.Begin()
			if err != nil {
				fmt.Println(err)
				return
			}
			_, err = tx.Exec(query, queryvalues...)
			if err != nil {
				fmt.Fprintln(w, err)
				tx.Rollback()
				return
			}
			err = tx.Commit()
			if err != nil {
				fmt.Fprintln(w, err)
				tx.Rollback()
				return
			}
			http.Redirect(w, r, "/admin/"+title, http.StatusSeeOther)

			return
		}
		cont := r.Context()
		uname := cont.Value(contextkey("uname")).(string)
		admintab := cont.Value(contextkey("adminTab")).(bool)
		status := headerdata{Loggedin: true, Title: title, Uname: uname, AdminTab: admintab}
		err = templ.ExecuteTemplate(w, "magicadd", status)
	}
}
