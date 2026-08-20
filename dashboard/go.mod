module git.ardenone.com/jedarden/zai-proxy/dashboard

go 1.23

require git.ardenone.com/jedarden/zai-proxy v0.0.0

replace git.ardenone.com/jedarden/zai-proxy => ../

require (
	github.com/prometheus/client_model v0.6.1
	github.com/prometheus/common v0.62.0
)

require (
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	google.golang.org/protobuf v1.36.1 // indirect
)
