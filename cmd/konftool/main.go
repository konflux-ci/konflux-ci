package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/web"
	"github.com/spf13/cobra"
)

func waitForTermSignal() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	signal.Notify(quit, syscall.SIGTERM)
	signal.Notify(quit, syscall.SIGHUP)
	signal.Notify(quit, syscall.SIGINT)
	<-quit
}

func mainCmd() cobra.Command {
	return cobra.Command{
		Use:   "konftool",
		Short: "A tool for installing and configuring konflux",
		Long: `"konftool" provides a web-based GUI for installing and
 configuring konflux-ci`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Add configuration option for this and detect free port
			listenAddr := "localhost:8000"

			web, err := web.Start(listenAddr)
			if err != nil {
				return err
			}
			defer func() {
				err = web.Stop()
				if err == nil {
					// "Goodbye" means we terminated nicely
					fmt.Println("Goodbye!")
				}
			}()
			fmt.Printf("Web server started on http://%s\n", listenAddr)
			waitForTermSignal()
			return err
		},
	}
}

func main() {
	cmd := mainCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Println("---", err)
		os.Exit(1)
	}
	os.Exit(0)
}
