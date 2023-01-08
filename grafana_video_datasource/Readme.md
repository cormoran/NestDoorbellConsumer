Simple HTTP server to serve saved clip preview image

```
go run main.go -directory <path to the root of nest doorbell consumer output>
# then, visit http://localhost:8080/list
#             http://localhost:8080/file/<rel path to file from the root of nest doorbell consumer output>
```

## How to use with Nest Doorbell Consumer & grafana

1. Setup Nest Doorbell Consumer and collect clip preview images.
2. Setup grafana and register [JSON API data source](https://grafana.com/grafana/plugins/marcusolsson-json-datasource/) with `URL=<this server's url>/list`.
3. Regiser dashboard variable with JSON API source registered in step2 with `field=$[*]` and params `from=${__from:date:seconds}` and `to=${__to:date:seconds}`.
4. Repeat [Video](https://grafana.com/grafana/plugins/innius-video-panel/) panel for variable registered in step3 and show video.
