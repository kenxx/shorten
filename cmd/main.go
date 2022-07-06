package main

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"math/bits"
	"net/http"
	"os"
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
    url_key    character varying(64) UNIQUE,
    custom_key character varying(64) UNIQUE,
    url        character varying(4096) NOT NULL CHECK ( LENGTH(url) > 0 ) UNIQUE,
    status     integer                 NOT NULL DEFAULT 0,
    expired_at timestamp,
    created_at timestamp               NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp               NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (url_key, url),
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
		QueryRow("SELECT id FROM "+TableName+" WHERE url = $1;", url).
		Scan(&id); err != nil {
		switch err {
		case sql.ErrNoRows:
			break
		default:
			log.Error(err)
			return "", err
		}
	}

	if id == 0 {
		if err := db.Connection.
			QueryRow("INSERT INTO "+TableName+" (url, custom_key) VALUES ($1, $2) RETURNING id;", url, customKey).
			Scan(&id); err != nil {
			log.Error(err)
			return "", err
		}
	}

	sum := sha256.Sum256([]byte(url))
	sumKey := uint64(sum[1]) + (uint64(sum[0]) << 8)
	baseFix := 64 - bits.LeadingZeros64(id)
	urlKey := strconv.FormatUint(id|sumKey<<baseFix, 36)

	if _, err := db.Connection.
		Query("UPDATE "+TableName+" SET url_key = $1 WHERE id = $2", urlKey, id); err != nil {
		return "", err
	}

	return urlKey, nil
}

func (db *Database) FindByKey(key string) (string, error) {
	var url string
	err := db.Connection.QueryRow("SELECT url FROM "+TableName+" WHERE url_key = $1 OR custom_key = $1", key).Scan(&url)
	if err != nil {
		switch err {
		case sql.ErrNoRows:
			return "", errors.New("url not found")
		default:
			return "", err
		}
	}

	return url, nil
}

func redirect(c echo.Context) error {
	key := c.Param("key")

	url, err := DB.FindByKey(key)
	if err != nil {
		log.Error(err)
		return err
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
		c.Logger().Error(err)
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

var ShortenHost = ""
var ShortenPort = 8080
var ShortenBasePath = "/"
var ShortenPostgresConnectionString = "host=localhost port=5432 user=root password=root database=root sslmode=disable"

func init() {
	if v := os.Getenv("SHORTEN_HOST"); v != "" {
		ShortenHost = v
	}
	if v := os.Getenv("SHORTEN_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Fatal(err)
		}
		ShortenPort = p
	}
	if v := os.Getenv("SHORTEN_BASE_PATH"); v != "" {
		ShortenBasePath = v
	}
	if v := os.Getenv("SHORTEN_POSTGRES"); v != "" {
		ShortenPostgresConnectionString = v
	}
}

func main() {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	if DB == nil {
		var err error
		if DB, err = NewDatabase(ShortenPostgresConnectionString); err != nil {
			log.Fatal(err)
		}
	}

	e.Static(ShortenBasePath, "public")
	e.File(ShortenBasePath+"favicon.ico", "public/favicon.ico")
	e.GET(ShortenBasePath+":key", redirect)
	e.POST(ShortenBasePath+"api/add-url", addUrl)
	e.Logger.Fatal(e.Start(fmt.Sprintf("%s:%d", ShortenHost, ShortenPort)))
}
