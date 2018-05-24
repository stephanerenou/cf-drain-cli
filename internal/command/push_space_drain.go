package command

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	flags "github.com/jessevdk/go-flags"

	"code.cloudfoundry.org/cli/plugin"
	"code.cloudfoundry.org/cli/plugin/models"
)

type Downloader interface {
	Download(assetName string) string
}

type RefreshTokenFetcher interface {
	RefreshToken() (string, error)
}

type pushSpaceDrainOpts struct {
	DrainName string `long:"drain-name" required:"true"`
	DrainURL  string `long:"drain-url" required:"true"`
	Path      string `long:"path"`
	DrainType string `long:"type"`
	Force     bool   `long:"force"`
}

func PushSpaceDrain(
	cli plugin.CliConnection,
	reader io.Reader,
	args []string,
	d Downloader,
	f RefreshTokenFetcher,
	log Logger,
) {
	opts := pushSpaceDrainOpts{
		DrainType: "all",
		Force:     false,
	}

	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	args, err := parser.ParseArgs(args)
	if err != nil {
		log.Fatalf("%s", err)
	}

	if len(args) > 0 {
		log.Fatalf("Invalid arguments, expected 0, got %d.", len(args))
	}

	if !opts.Force {
		log.Print(
			"The space drain functionality is an experimental feature. ",
			"See https://github.com/cloudfoundry/cf-drain-cli#space-drain-experimental for more details.\n",
			"Do you wish to proceed? [y/N] ",
		)

		buf := bufio.NewReader(reader)
		resp, err := buf.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read user input: %s", err)
		}
		if strings.TrimSpace(strings.ToLower(resp)) != "y" {
			log.Fatalf("OK, exiting.")
		}
	}

	pushDrain(cli, "space-drain", "space_drain", nil, opts, d, f, log)
}

func pushDrain(cli plugin.CliConnection, appName, command string, extraEnvs [][]string, opts pushSpaceDrainOpts, d Downloader, f RefreshTokenFetcher, log Logger) {
	if opts.Path == "" {
		log.Printf("Downloading latest space drain from github...")
		opts.Path = path.Dir(d.Download(command))
		log.Printf("Done downloading space drain from github.")
	}

	_, err := cli.CliCommand(
		"push", appName,
		"-p", opts.Path,
		"-b", "binary_buildpack",
		"-c", fmt.Sprint("./", command),
		"--health-check-type", "process",
		"--no-start",
		"--no-route",
	)
	if err != nil {
		log.Fatalf("%s", err)
	}

	space := currentSpace(cli, log)
	api := apiEndpoint(cli, log)

	skipCertVerify, err := cli.IsSSLDisabled()
	if err != nil {
		log.Fatalf("%s", err)
	}

	refreshToken, err := f.RefreshToken()
	if err != nil {
		log.Fatalf("%s", err)
	}

	sharedEnvs := [][]string{
		{"SPACE_ID", space.Guid},
		{"DRAIN_NAME", opts.DrainName},
		{"DRAIN_URL", opts.DrainURL},
		{"DRAIN_TYPE", opts.DrainType},
		{"API_ADDR", api},
		{"UAA_ADDR", strings.Replace(api, "api", "uaa", 1)},
		{"CLIENT_ID", "cf"},
		{"REFRESH_TOKEN", refreshToken},
		{"SKIP_CERT_VERIFY", strconv.FormatBool(skipCertVerify)},
		{"DRAIN_SCOPE", "space"},
	}

	envs := append(sharedEnvs, extraEnvs...)
	for _, env := range envs {
		_, err := cli.CliCommandWithoutTerminalOutput("set-env", appName, env[0], env[1])
		if err != nil {
			log.Fatalf("%s", err)
		}
	}

	cli.CliCommand("start", appName)
}

func currentSpace(cli plugin.CliConnection, log Logger) plugin_models.Space {
	space, err := cli.GetCurrentSpace()
	if err != nil {
		log.Fatalf("%s", err)
	}
	return space
}

func apiEndpoint(cli plugin.CliConnection, log Logger) string {
	api, err := cli.ApiEndpoint()
	if err != nil {
		log.Fatalf("%s", err)
	}
	return api
}
