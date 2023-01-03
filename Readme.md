# Nest Doorbell Consumer

This program saves short clip preview image when nest doorbell detect events.

Please refer official guide (https://developers.google.com/nest/device-access/get-started) to setup the account.

1. Create nest device project in https://console.nest.google.com/device-access/.
   Pass project id to program like `-nest-project-id enterprises/<project_id>`
2. Create oauth token for smart device in google could project
   Pass credential json path like `-smart-device-cred-path <credentials.json>`
3. Create google could project for pubsub
   Pass it like `-pubsub-project-id <google could project id>`
4. Create pubsub subscription against pubsub topic given in the nest device project
   Pass it like `-pubsub-subscription-id <subscription name>`
5. Create google cloud service account for pubsub
   Pass credential json path like `-pubsub-cred-path <pub-sub-client-key-<google cloud project id>-hoge.json>`
6. Run program like `go run main.go <args> -output-dir output`
