//go:build unreleased

package cli

const packageUsage = `  cockpit install <package-dir> [--allow]
                   take a package in and say so; --allow also admits its templates and queries
  cockpit workflow propose <result>...   what the runs behind results offer, numbered
  cockpit workflow extract <result>... --name NAME --reference RESULT [--hole NAME=N]... [--param NAME=N]...
  cockpit workflow list                  the installed workflows
  cockpit workflow show <name>           steps, holes, parameters, reference
  cockpit workflow run <name> [--check] [--bind NAME=VALUE]...
  cockpit workflow trace <result>        the steps behind a result, in the order they ran


`
