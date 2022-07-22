# Shorten

Shorten is a simple short url service written by golang.

# Install

```shell
# setup listen host
export SHORTEN_HOST="<listen host>" # default is empty

# setup listen port
export SHORTEN_PORT="<listen port>" # default is 8080

# setup short url path
export SHORTEN_BASE_PATH="<shorten base path>" # default is empty string
## e.g. set to "/test/" then your short url is "https://<host>/test/<auto_key>"

# setup postgres connection string
export SHORTEN_POSTGRES="<postgres connection string>" # default is root@localhost
## default is "host=localhost port=5432 user=root password=root database=root sslmode=disable"

# install pkg
go mod tidy

# build
go build cmd/shorten/shorten.go

# run
./shorten
```

# Todo

- [ ] Memory Cache
- [ ] Redis Cache
- [x] Support [MyUrls](https://github.com/CareyWang/MyUrls) protocol.
