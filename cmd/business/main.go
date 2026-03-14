package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	_ "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator/builtin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/app"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/config"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/copilotgate"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/logging"

	log "github.com/sirupsen/logrus"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

const (
	exitCodeOK          = 0
	exitCodeError       = 1
	exitCodeGateBlocked = 2
)

// init initializes the shared logger setup.
func init() {
	logging.SetupBaseLogger()
	buildinfo.Version = Version
	buildinfo.Commit = Commit
	buildinfo.BuildDate = BuildDate
}

// main runs the CLI entrypoint and exits on unrecoverable command errors.
func main() {
	fmt.Printf("CLIProxyAPIBusiness Version: %s, Commit: %s, BuiltAt: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)
	os.Exit(run(os.Args[1:]))
}

// run executes supported subcommands and returns process exit code.
func run(args []string) int {
	if len(args) > 0 && strings.EqualFold(args[0], "gate") {
		return runGate(args[1:])
	}

	if err := runServer(context.Background(), args); err != nil {
		log.WithError(err).Error("command failed")
		return exitCodeError
	}
	return exitCodeOK
}

func runGate(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: cpab gate copilot-dualrun --report <file>")
		return exitCodeError
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "copilot-dualrun":
		return runGateCopilotDualRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown gate command: %s\n", args[0])
		return exitCodeError
	}
}

func runGateCopilotDualRun(args []string) int {
	fs := flag.NewFlagSet("copilot-dualrun", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	report := fs.String("report", "", "path to 7-day summary report json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parse gate arguments: %v\n", err)
		return exitCodeError
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unexpected positional args: %s\n", strings.Join(fs.Args(), " "))
		return exitCodeError
	}
	if strings.TrimSpace(*report) == "" {
		fmt.Fprintln(os.Stderr, "missing required flag: --report")
		return exitCodeError
	}

	result, err := copilotgate.EvaluateReportFile(*report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evaluate gate report failed: %v\n", err)
		return exitCodeError
	}

	fmt.Printf("copilot_dualrun_gate window_days=%d days_in_window=%d critical_diff_new=%d undeclared_passthrough=%d\n", result.WindowDays, result.DaysInWindow, result.CriticalDiffNew, result.UndeclaredPassthrough)
	if result.Pass {
		return exitCodeOK
	}
	return exitCodeGateBlocked
}

// runServer parses flags, loads config, and starts the init or main server.
func runServer(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "config file path (or env CONFIG_PATH)")
	port := fs.Int("port", 8318, "server port (used for init server and initial config)")
	if errParse := fs.Parse(args); errParse != nil {
		return errParse
	}

	if errValidate := validatePort(*port); errValidate != nil {
		return errValidate
	}

	appCfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*cfgPath) != "" {
		appCfg.ConfigPath = config.ResolveConfigPath(*cfgPath)
	}

	configPath := config.ResolveConfigPath(appCfg.ConfigPath)
	if !app.ConfigExists(configPath) && strings.TrimSpace(os.Getenv(config.EnvDBConnection)) == "" {
		log.Info("config.yaml not found, starting init server...")
		errInit := app.RunInitServer(ctx, appCfg, *port)
		if errors.Is(errInit, app.ErrInitCompleted) {
			log.Info("initialization completed, starting main server...")
			return app.RunServer(ctx, appCfg, *port)
		}
		return errInit
	}

	return app.RunServer(ctx, appCfg, *port)
}

func validatePort(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port: %d", port)
	}
	return nil
}
