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
  or path = "third_party/otelzap/otelzap.go" and name = "log"
  or path = "third_party/otelzap/logvalue.go" and name = "logValue"
}

from string path, string name
where expected(path, name)
select path, name,
  count(FuncDecl declaration |
    declaration.getFile().getRelativePath() = path and
    declaration.getName() = name and
    exists(declaration.getBody())
  ) as bodies
