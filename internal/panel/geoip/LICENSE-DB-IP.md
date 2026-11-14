# country.bin.gz

`country.bin.gz` is derived from the DB-IP "IP to Country Lite" database by
`scripts/geoipgen` (ranges converted to a compact table; IPv6 kept at /64
granularity). The release is recorded inside the file and returned by
`geoip.Version()`.

IP Geolocation by DB-IP — <https://db-ip.com>

The DB-IP Lite databases are licensed under the
[Creative Commons Attribution 4.0 International License](https://creativecommons.org/licenses/by/4.0/).
The panel shows this attribution in 系统设置 → 系统信息.

## Refreshing

DB-IP publishes a new Lite release at the start of each month:

```sh
mkdir -p /tmp/dbip && cd /tmp/dbip
curl -fsSLO https://download.db-ip.com/free/dbip-country-lite-YYYY-MM.csv.gz
cd -   # back to the repository root
go run ./scripts/geoipgen -in /tmp/dbip/dbip-country-lite-YYYY-MM.csv.gz \
    -version YYYY-MM -out internal/panel/geoip/country.bin.gz
go test ./internal/panel/geoip
```
