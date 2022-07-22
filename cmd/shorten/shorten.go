package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"math/bits"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

const TableName = "urls"

const TableExists = `SELECT exists(
	SELECT FROM information_schema.tables 
		WHERE table_schema = 'public' AND table_type LIKE 'BASE TABLE' AND table_name = '` + TableName + `'
);`

const CreateTable = `CREATE TABLE ` + TableName + `
(
    id         serial PRIMARY KEY,
    auto_key   character varying(64) UNIQUE,
    custom_key character varying(64) UNIQUE,
    url        character varying(4096) NOT NULL CHECK ( LENGTH(url) > 0 ) UNIQUE,
    status     integer                 NOT NULL DEFAULT 0,
    expired_at timestamp,
    created_at timestamp               NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp               NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (auto_key, url),
    UNIQUE (custom_key, url)
);`

var SupportedPrefix = []string{"https://", "http://"}

var DB *Database

type Database struct {
	Migrated   bool
	Connection *sql.DB
}

func NewDatabase(connStr string) (*Database, error) {
	db := new(Database)
	err := db.Init(connStr)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func (db *Database) Init(connStr string) error {
	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}

	db.Connection = conn

	var exists bool
	if err = db.Connection.QueryRow(TableExists).Scan(&exists); err != nil {
		log.Error(err)
		return err
	}
	if !exists {
		if _, err = db.Connection.Query(CreateTable); err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}

func (db *Database) AddUrl(url string, customKey string) (string, error) {
	url = strings.Trim(url, "\t\n ")
	supported := false
	for _, prefix := range SupportedPrefix {
		if strings.HasPrefix(url, prefix) {
			supported = true
			break
		}
	}

	if !supported {
		return "", errors.New("url not supported")
	}

	var id uint64
	if err := db.Connection.
		QueryRow("SELECT id FROM "+TableName+" WHERE url = $1 LIMIT 1;", url).
		Scan(&id); err != nil {
		switch err {
		case sql.ErrNoRows:
			break
		default:
			return "", err
		}
	}

	if id == 0 {
		if err := db.Connection.
			QueryRow("INSERT INTO "+TableName+" (url) VALUES ($1) RETURNING id;", url).
			Scan(&id); err != nil {
			return "", err
		}
	}

	sum := sha256.Sum256([]byte(url))
	sumKey := uint64(sum[1]) + (uint64(sum[0]) << 8)
	baseFix := 64 - bits.LeadingZeros64(id)
	urlKey := strconv.FormatUint(id|sumKey<<baseFix, 36)

	if customKey == "" {
		customKey = urlKey
	}

	if _, err := db.Connection.
		Query("UPDATE "+TableName+" SET auto_key = $1, custom_key = $2 WHERE id = $3", urlKey, customKey, id); err != nil {
		return "", err
	}

	return urlKey, nil
}

func (db *Database) FindByKey(key string) (string, bool) {
	var url string
	err := db.Connection.QueryRow("SELECT url FROM "+TableName+" WHERE auto_key = $1 OR custom_key = $1 ORDER BY id DESC LIMIT 1", key).Scan(&url)
	if err != nil {
		switch err {
		case sql.ErrNoRows:
			return "", false
		default:
			log.Error(err)
			return "", false
		}
	}

	return url, true
}

func redirect(c echo.Context) error {
	key := c.Param("key")

	url, ok := DB.FindByKey(key)
	if !ok {
		log.Error("find key(" + key + ") error")
		url = "/"
	}

	return c.Redirect(http.StatusMovedPermanently, url)
}

type Result struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type AddURLRequest struct {
	URL       string `json:"url" validate:"required"`
	CustomKey string `json:"custom_key"`
}

func addUrl(c echo.Context) error {
	url := new(AddURLRequest)
	err := c.Bind(url)
	if err != nil {
		return err
	}
	var urlKey string
	if urlKey, err = DB.AddUrl(url.URL, url.CustomKey); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, &Result{
		Code:    0,
		Message: "",
		Data: map[string]string{
			"uri": ShortenBasePath + urlKey,
		},
	})
}

func supportMyURLS(c echo.Context) error {
	longURL := c.FormValue("longUrl")
	shortKey := "" // we don't support custom key

	_longUrl, _ := base64.StdEncoding.DecodeString(longURL)
	longURL = string(_longUrl)

	response := make(map[string]interface{})
	if longURL == "" {
		response["Code"] = 0
		response["Message"] = "pls enter long url"
		return c.JSON(http.StatusBadRequest, &response)
	}

	var urlKey string
	var err error
	if urlKey, err = DB.AddUrl(longURL, shortKey); err != nil {
		response["Code"] = 0
		response["Message"] = "something bad"
		return c.JSON(http.StatusBadRequest, &response)
	}

	response["Code"] = 1
	response["Message"] = ""
	response["LongUrl"] = longURL
	response["ShortUrl"] = strings.TrimRight(ShortenBaseUrl, "/") + "/" + urlKey
	return c.JSON(http.StatusOK, &response)
}

var ShortenHost = ""
var ShortenPort = 8080
var ShortenBasePath = "/"
var ShortenPostgresConnectionString = "host=localhost port=5432 user=root password=root database=root sslmode=disable"
var ShortenPrefix = "./"
var ShortenBaseUrl = ""

func init() {
	if v := os.Getenv("SHORTEN_HOST"); v != "" {
		ShortenHost = v
		log.Info("host is set to " + ShortenHost)
	}
	if v := os.Getenv("SHORTEN_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Fatal(err)
		}
		ShortenPort = p
		log.Info(fmt.Sprintf("host is set to %d", ShortenPort))
	}
	if v := os.Getenv("SHORTEN_BASE_PATH"); v != "" {
		ShortenBasePath = "/" + strings.Trim(v, "/") + "/"
		log.Info("base path is set to " + ShortenBasePath)
	}
	if v := os.Getenv("SHORTEN_POSTGRES"); v != "" {
		ShortenPostgresConnectionString = v
		log.Info("postgres is set")
	}
	if v := os.Getenv("SHORTEN_PREFIX"); v != "" {
		ShortenPrefix = v
		log.Info("prefix is set")
	}
	if v := os.Getenv("SHORTEN_BASE_URL"); v != "" {
		ShortenBaseUrl = v
		log.Info("base url is set")
	}
}

func main() {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	if DB == nil {
		var err error
		if DB, err = NewDatabase(ShortenPostgresConnectionString); err != nil {
			log.Fatal(err)
		}
	}

	e.HTTPErrorHandler = func(err error, c echo.Context) {
		code := http.StatusInternalServerError
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}
		c.Logger().Error(err)
		if err = c.JSON(code, Result{
			Code:    code,
			Message: "System Error: " + err.Error(),
		}); err != nil {
			c.Logger().Error(err)
		}
	}

	e.Static("/", path.Join(ShortenPrefix, "public"))
	e.File("/favicon.ico", path.Join(ShortenPrefix, "public/favicon.ico"))
	e.GET(ShortenBasePath+":key", redirect)
	e.POST("/api/add-url", addUrl)
	e.POST("/short", supportMyURLS)

	e.Logger.Fatal(e.Start(fmt.Sprintf("%s:%d", ShortenHost, ShortenPort)))
}
