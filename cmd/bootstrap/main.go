package bootstrap

import "mmn/bootstrap"

func main() {
	bootstrap.InitBootstrapNode()
	select {}
}
