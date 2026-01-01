package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/ipc"
	"github.com/sandeepkv93/googlysync/internal/ipc/gen"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	ping := flag.Bool("ping", false, "ping daemon and print version")
	status := flag.Bool("status", false, "print daemon status")
	socketPath := flag.String("socket", "", "unix socket path")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if *ping || *status {
		cfg, err := config.NewConfigWithOptions(config.Options{SocketPath: *socketPath})
		if err != nil {
			fmt.Printf("config error: %v\n", err)
			return
		}
		conn, err := ipc.Dial(context.Background(), cfg.SocketPath)
		if err != nil {
			fmt.Printf("dial error: %v\n", err)
			return
		}
		defer conn.Close()

		if *ping {
			client := gen.NewDaemonControlClient(conn)
			resp, err := client.Ping(context.Background(), &gen.Empty{})
			if err != nil {
				fmt.Printf("ping error: %v\n", err)
				return
			}
			fmt.Println(resp.Version)
			return
		}

		if *status {
			client := gen.NewSyncStatusClient(conn)
			resp, err := client.GetStatus(context.Background(), &gen.Empty{})
			if err != nil {
				fmt.Printf("status error: %v\n", err)
				return
			}
			fmt.Printf("%s: %s\n", resp.Status.State.String(), resp.Status.Message)
			return
		}
	}

	fmt.Println("drive-ui placeholder: no UI wired yet")
}
