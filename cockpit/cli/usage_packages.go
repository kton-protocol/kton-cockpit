package cli

const packageUsage = `  cockpit install <package-dir> [--allow]
                   take a package in and say so; --allow also admits its templates and queries
  cockpit workflow propose <result>...   what the runs behind results offer, numbered
  cockpit workflow extract <result>... --name NAME --reference RESULT [--hole NAME=N]... [--param NAME=N]...
  cockpit workflow list                  the installed workflows
  cockpit workflow show <name>           steps, holes, parameters, reference
  cockpit workflow run <name> [--check] [--bind NAME=VALUE]...
  cockpit workflow trace <result>        the steps behind a result, in the order they ran
  cockpit ui list [--json]               the installed packages for a frontend: allowed, and what fails its check
  cockpit ui cat <package> <sha256:hash> a member of a usable package's sealed revision, its bytes on stdout
  cockpit ui cat <package> <sha256:hash>... --json  several members in one call: {package, name, dir, members: [{hash, bytes (base64)}]}


`
