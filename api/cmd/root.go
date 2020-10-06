package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"hddcheap/pkg"
)

func init() {
	rootCmd.PersistentFlags().StringVar(&verbosity, "verbosity", "debug", "a logrus logging level name")
	rootCmd.PersistentFlags().IntVar(&refreshPeriod, "period", 600, "the period of time in seconds between product listing refreshes")
	rootCmd.PersistentFlags().IntVar(&numPages, "pages", 3, "the number of Amazon search result pages to scan")
	rootCmd.PersistentFlags().StringVar(&listenAddr, "addr", "0.0.0.0:3001", "The port for the API to listen on")
}

var (
	verbosity     string
	refreshPeriod int
	numPages      int
	listenAddr    string

	rootCmd = &cobra.Command{
		Use:   "hddcheap",
		Short: "The backend for the hddcheap application",
		Long:  "hddcheap is a single page webapp for quickly finding cheap spinning rust on Amazon",
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := log.ParseLevel(verbosity)
			if err != nil {
				return err
			}
			log.SetLevel(level)
			log.SetReportCaller(true)
			pkg.Serve(refreshPeriod, numPages, listenAddr)
			return nil
		},
	}
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf(err.Error())
	}
}
