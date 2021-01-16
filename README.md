# homechat

A chat server.

contains a few bots and a youtube music server [frizinak/libym](https://github.com/frizinak/libym)

random list of features:

- upload: cli and web
- wasm: because that's obviously a feature
- [wttr.in](http://wttr.in/)
- hue lights control using [amimof/huego](https://github.com/amimof/huego) and simple wrapper [frizinak/hue](https://github.com/frizinak/hue)

non features:

- notifications: only cli since it's a pain in browsers
- encryption: messages can be tampered with, no MAC yet

roadmap:

- tls: disables internal crypto
- add bots

## local server

`make serve-live`

compile and listen on 127.0.0.1:1200 (storage in /tmp/homechat)

`make local`

runs a local client that will connect to 127.0.0.1:1200 (config in ./testclient)
