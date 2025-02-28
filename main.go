package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	cacheutil "github.com/bitrise-io/go-steputils/cache"
	"github.com/bitrise-io/go-steputils/input"
	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/env"
	"github.com/bitrise-io/go-utils/filedownloader"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-steplib/steps-carthage/cachedcarthage"
	"github.com/bitrise-steplib/steps-carthage/carthage"
	"github.com/hashicorp/go-version"
	"github.com/kballard/go-shellquote"
)

const (
	projectDirArg = "--project-directory"
)

// FileProvider ...
type FileProvider interface {
	LocalPath(path string) (string, error)
}

// Config ...
type Config struct {
	GithubAccessToken stepconf.Secret `env:"github_access_token"`
	CarthageCommand   string          `env:"carthage_command,required"`
	CarthageOptions   string          `env:"carthage_options"`
	SourceDir         string          `env:"BITRISE_SOURCE_DIR"`
	Xcconfig          string          `env:"xcconfig"`
	XcconfigFromEnv   string          `env:"XCODE_XCCONFIG_FILE"`

	// Debug
	VerboseLog bool `env:"verbose_log,opt[yes,no]"`
}

func fail(format string, v ...interface{}) {
	log.Errorf(format, v...)
	os.Exit(1)
}

func main() {
	var configs Config
	if err := stepconf.NewInputParser(env.NewRepository()).Parse(&configs); err != nil {
		fail("Could not create config: %s", err)
	}
	stepconf.Print(configs)

	log.SetEnableDebugLog(configs.VerboseLog)

	// Environment
	fmt.Println()
	log.Infof("Environment:")

	carthageVersion, err := getCarthageVersion()
	if err != nil {
		fail("Failed to get carthage version, error: %s", err)
	}
	log.Printf("- CarthageVersion: %s", carthageVersion.String())

	swiftVersion, err := getSwiftVersion()
	if err != nil {
		fail("Failed to get swift version, error: %s", err)
	}
	log.Printf("- SwiftVersion: %s", strings.Replace(swiftVersion, "\n", "- ", -1))
	// --

	// Parse options
	args := parseCarthageOptions(configs)
	fileProvider := input.NewFileProvider(filedownloader.New(http.DefaultClient))
	xconfigPath, err := parseXCConfigPath(configs.Xcconfig, configs.XcconfigFromEnv, fileProvider)
	if err != nil {
		fail("Failed to get xcconfig file, error: %s", err)
	}

	projectDir := parseProjectDir(configs.SourceDir, args)
	project := cachedcarthage.NewProject(projectDir)
	filecache := cacheutil.New()
	stateProvider := cachedcarthage.DefaultStateProvider{}

	runner := cachedcarthage.NewRunner(
		configs.CarthageCommand,
		args,
		configs.GithubAccessToken,
		xconfigPath,
		cachedcarthage.NewCache(project, swiftVersion, &filecache, stateProvider),
		carthage.NewCLIBuilder(),
	)
	if err := runner.Run(); err != nil {
		fail("Failed to execute step: %s", err)
	}
}

func parseXCConfigPath(pathFromStepInput string, pathFromEnv string, fileProvider FileProvider) (string, error) {
	pathToUse := ""
	if pathFromStepInput != "" {
		localPath, err := fileProvider.LocalPath(pathFromStepInput)
		if err != nil {
			return "", err
		}
		pathToUse = localPath
	}

	if pathFromEnv != "" {
		if pathToUse != "" {
			log.Warnf("Both `xcconfig` input and `XCODE_XCCONFIG_FILE` are set. Using `xcconfig` input.")
		} else {
			pathToUse = pathFromEnv
		}
	}

	return pathToUse, nil
}

func parseCarthageOptions(config Config) []string {
	var customCarthageOptions []string
	if config.CarthageOptions != "" {
		options, err := shellquote.Split(config.CarthageOptions)
		if err != nil {
			fail("Failed to shell split CarthageOptions (%s), error: %s", config.CarthageOptions)
		}
		customCarthageOptions = options
	}
	return customCarthageOptions
}

func getCarthageVersion() (*version.Version, error) {
	cmd := carthage.NewCLIBuilder().Append("version").Command(nil, nil)
	out, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		return nil, err
	}

	// if output is multi-line, get the last line of string
	// parse Version from cmd output
	for _, outLine := range strings.Split(out, "\n") {
		if currentVersion, err := version.NewVersion(outLine); err == nil {
			return currentVersion, nil
		}
	}

	return nil, fmt.Errorf("failed to parse `$ carthage version` output: %s", out)
}

func getSwiftVersion() (string, error) {
	cmd := command.NewFactory(env.NewRepository()).Create("swift", []string{"-version"}, nil)
	return cmd.RunAndReturnTrimmedCombinedOutput()
}

func parseProjectDir(originalDir string, customCarthageOptions []string) string {
	projectDir := originalDir

	isNextOptionProjectDir := false
	for _, option := range customCarthageOptions {
		if option == projectDirArg {
			isNextOptionProjectDir = true
			continue
		}

		if isNextOptionProjectDir {
			projectDir = option

			fmt.Println()
			log.Infof("--project-directory flag found with value: %s", projectDir)
			log.Printf("using %s as working directory", projectDir)

			break
		}
	}

	return projectDir
}
