import go

predicate expected(string path, string name) {
  path = "main.go" and name = "main"
  or path = "desktop/main.go" and name = "main"
  or path = "desktop/main.go" and name = "run"
  or path = "desktop/env_other.go" and name = "hydrateLoginShellEnv"
  or path = "desktop/menu.go" and name = "installApplicationMenu"
  or path = "desktop/deeplink.go" and name = "handleDeepLink"
  or path = "desktop/notify.go" and name = "watchClusterStatus"
  or path = "desktop/window_state.go" and name = "trackWindowState"
  or path = "third_party/cel-go/cel/env.go" and name = "NewEnv"
  or path = "third_party/glamour/glamour.go" and name = "NewTermRenderer"
  or path = "third_party/go-macholibre/universal_binary.go" and name = "ExtractReaders"
  or path = "third_party/kyverno-jmespath/api.go" and name = "Search"
  or path = "third_party/jmespath/api.go" and name = "Search"
  or path = "third_party/ansi/width.go" and name = "Strip"
  or path = "third_party/ansi-runtime/width.go" and name = "Strip"
  or path = "third_party/redisotel/tracing.go" and name = "InstrumentTracing"
  or path = "third_party/rediscmd/rediscmd.go" and name = "CmdString"
  or path = "third_party/dynamiclistener/cert/cert.go" and name = "NewPrivateKey"
  or path = "third_party/dynamiclistener/factory/cert_utils.go" and name = "ParseCertPEM"
  or path = "third_party/otelzap/otelzap.go" and name = "log"
  or path = "third_party/otelzap/logvalue.go" and name = "logValue"
  or path = "internal/codeqlprofile/metrics.go" and name = "ParseTime"
  or path = "internal/codeqlprofile/metrics.go" and name = "Summarize"
  or path = "internal/codeqlprofile/files.go" and name = "ReadMeasurements"
  or path = "internal/codeqlprofile/cmd/main.go" and name = "main"
}

from string path, string name
where expected(path, name)
select path, name,
  count(FuncDecl declaration |
    declaration.getFile().getRelativePath() = path and
    declaration.getName() = name and
    exists(declaration.getBody())
  ) as bodies
