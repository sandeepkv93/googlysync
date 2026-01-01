package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/ipc"
	ipcgen "github.com/sandeepkv93/googlysync/internal/ipc/gen"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		runTUI(os.Args[1:])
		return
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon(os.Args[2:])
	case "ping":
		runPing(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "fuse":
		runFuse(os.Args[2:])
	case "version":
		fmt.Println(version)
	case "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("Usage: googlysync <command> [options]")
	fmt.Println("Commands:")
	fmt.Println("  daemon   Start the sync daemon")
	fmt.Println("  ping     Ping the daemon and print version")
	fmt.Println("  status   Launch status TUI")
	fmt.Println("  fuse     Placeholder for streaming mode")
	fmt.Println("  version  Print CLI version")
	fmt.Println("  help     Show this help")
	fmt.Println("(No command opens the status TUI)")
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file (JSON)")
	logLevel := fs.String("log-level", "", "log level")
	socketPath := fs.String("socket", "", "unix socket path")
	_ = fs.Parse(args)

	opts := config.Options{
		ConfigPath: *configPath,
		LogLevel:   *logLevel,
		SocketPath: *socketPath,
	}

	daemon, err := InitializeDaemon(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
		os.Exit(1)
	}
	if daemon.Logger != nil {
		defer daemon.Logger.Sync()
	}
	if daemon.IPC != nil {
		daemon.IPC.WithVersion(version)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := daemon.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		os.Exit(1)
	}
}

func runPing(args []string) {
	fs := flag.NewFlagSet("ping", flag.ExitOnError)
	socketPath := fs.String("socket", "", "unix socket path")
	timeout := fs.Duration("timeout", 3*time.Second, "timeout for request")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cfg, err := config.NewConfigWithOptions(config.Options{SocketPath: *socketPath})
	if err != nil {
		fmt.Printf("config error: %v\n", err)
		return
	}
	conn, err := ipc.Dial(ctx, cfg.SocketPath)
	if err != nil {
		fmt.Printf("dial error: %v\n", err)
		return
	}
	defer conn.Close()

	client := ipcgen.NewDaemonControlClient(conn)
	resp, err := client.Ping(ctx, &ipcgen.Empty{})
	if err != nil {
		fmt.Printf("ping error: %v\n", err)
		return
	}
	fmt.Println(resp.Version)
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	socketPath := fs.String("socket", "", "unix socket path")
	interval := fs.Duration("interval", 2*time.Second, "refresh interval")
	once := fs.Bool("once", false, "print status once and exit")
	_ = fs.Parse(args)

	if *once {
		printStatusOnce(*socketPath)
		return
	}

	m := newModel(*socketPath, *interval)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Printf("ui error: %v\n", err)
	}
}

func printStatusOnce(socketPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg, err := config.NewConfigWithOptions(config.Options{SocketPath: socketPath})
	if err != nil {
		fmt.Printf("config error: %v\n", err)
		return
	}
	conn, err := ipc.Dial(ctx, cfg.SocketPath)
	if err != nil {
		fmt.Printf("dial error: %v\n", err)
		return
	}
	defer conn.Close()

	client := ipcgen.NewSyncStatusClient(conn)
	resp, err := client.GetStatus(ctx, &ipcgen.Empty{})
	if err != nil {
		fmt.Printf("status error: %v\n", err)
		return
	}
	if resp == nil || resp.Status == nil {
		fmt.Println("UNKNOWN: no status")
		return
	}
	fmt.Printf("%s: %s\n", resp.Status.State.String(), resp.Status.Message)
}

func runTUI(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	socketPath := fs.String("socket", "", "unix socket path")
	interval := fs.Duration("interval", 2*time.Second, "refresh interval")
	_ = fs.Parse(args)

	m := newModel(*socketPath, *interval)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Printf("ui error: %v\n", err)
	}
}

func runFuse(args []string) {
	_ = args
	fmt.Println("fuse placeholder: streaming mode not implemented")
}
