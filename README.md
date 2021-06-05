# homechat

![screenshot-music](https://raw.githubusercontent.com/frizinak/homechat/master/music.png)

![screenshot-chat](https://raw.githubusercontent.com/frizinak/homechat/master/chat.png)

A chat server.

contains a few bots and a youtube music server [frizinak/libym](https://github.com/frizinak/libym)

random list of features:

- upload: cli and web
- wasm: because that's obviously a feature
- [wttr.in](http://wttr.in/)
- hue lights control using [amimof/huego](https://github.com/amimof/huego) and simple wrapper [frizinak/hue](https://github.com/frizinak/hue)
- homechat musicnode [-low-latency]: run a replicated music player in sync with the server
- inline images using ueberzug (ueberzug branch) (X11 only)

non features:

- notifications: only cli since it's a pain in browsers
- homechat musicnode [-low-latency]: run a replicated music player not really in sync with the server

roadmap:

- tls: disables internal crypto
- add bots

## Dependencies

server and `homechat music {node,client}`:

- mpv
- youtube-dl
- ffmpeg

## local server

`make serve-live`

compile and listen on 127.0.0.1:120{2,3} (storage in /tmp/homechat)

`make local`

compile and run a local client that will connect to 127.0.0.1:120{2 or 3} (config in ./testclient)

