package conf

import (
	"os"
	"strings"
)

// EnrichDataFromEnv overlays SATH_MYSQL_DSN or replaces the password via SATH_MYSQL_PASSWORD.
func EnrichDataFromEnv(d *Data) {
	if d == nil {
		return
	}
	if dsn := strings.TrimSpace(os.Getenv("SATH_MYSQL_DSN")); dsn != "" {
		if d.Database == nil {
			d.Database = &Data_Database{}
		}
		d.Database.Source = dsn
		return
	}
	pw := strings.TrimSpace(os.Getenv("SATH_MYSQL_PASSWORD"))
	if pw == "" || d.Database == nil {
		return
	}
	src := strings.TrimSpace(d.Database.GetSource())
	if src == "" {
		return
	}
	d.Database.Source = replaceMySQLDSNPassword(src, pw)
}

func replaceMySQLDSNPassword(dsn, password string) string {
	at := strings.Index(dsn, "@")
	if at <= 0 {
		return dsn
	}
	userinfo := dsn[:at]
	colon := strings.Index(userinfo, ":")
	if colon < 0 {
		return dsn
	}
	return userinfo[:colon+1] + password + dsn[at:]
}
