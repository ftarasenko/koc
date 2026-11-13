package keyvrm

import (
	"context"
	"io"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/spf13/cobra"

	"github.com/ftarasenko/koc/internal/auth"
	"github.com/ftarasenko/koc/internal/output"
	keyvrmapi "github.com/ftarasenko/koc/pkg/keyvrm"
)

func newMonitoringCommand(a *auth.Options, o *output.Options) *cobra.Command {
	cmd := &cobra.Command{Use: "monitoring", Short: "KeyVRM monitoring status"}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the monitoring overview (enabled state and counters)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.Validate(); err != nil {
				return err
			}
			sc, err := newKeyVRMClient(cmd.Context(), a)
			if err != nil {
				return err
			}
			return runMonitoringShow(cmd.Context(), sc, o, cmd.OutOrStdout())
		},
	})
	return cmd
}

func runMonitoringShow(ctx context.Context, sc *gophercloud.ServiceClient, o *output.Options, w io.Writer) error {
	m, err := keyvrmapi.GetMonitoringOverview(ctx, sc)
	if err != nil {
		return err
	}
	fields, values := monitoringOverviewView(m)
	return o.WriteSingle(w, fields, values)
}
