package transport

import (
 "encoding/json"
 "html"
)

// securityLoginWidget contains no credential fields or privileged tokens.
// It opens an independently authenticated top-level HTTPS origin.
func (s *Server) securityLoginWidget() string {
  encoded, _ := json.Marshal(s.securityLoginURL)
  return "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><style>\n:root{font-family:system-ui,sans-serif;color:inherit}body{margin:0;padding:16px}.card{border:1px solid #838b9866;border-radius:12px;padding:18px;max-width:430px}h2{font-size:1.15rem;margin:0 0 8px}p{font-size:.9rem;line-height:1.5;color:inherit;opacity:.8}button{display:block;border:0;border-radius:7px;background:#f0f0f0;color:#161616;padding:11px 16px;font:inherit;cursor:pointer;margin:14px 0}a{font-size:.85rem;color:inherit}small{display:block;font-size:.77rem;opacity:.7;margin-top:12px}\n</style></head><body><section class=\"card\"><h2>Homelab administrator sign-in</h2><p>Authenticate with your administrative password and authenticator on a protected page. Your credentials remain outside ChatGPT.</p><button id=\"sign-in\" type=\"button\">Open secure sign-in</button><a href=\"" + html.EscapeString(s.securityLoginURL) +
    "\" target=\"_blank\" rel=\"noopener noreferrer\">Open protected page in browser</a><small>Authentication expires after 120 minutes of inactivity or 12 hours, whichever comes first. Destructive changes need separate confirmation.</small></section><script>\nconst url = " + string(encoded) +
    ";\ndocument.getElementById(\"sign-in\").addEventListener(\"click\",() => {\n  if (window.openai && typeof window.openai.openExternal === \"function\") {\n    window.openai.openExternal({href:url,redirectUrl:false});\n  } else {\n    window.open(url,\"_blank\",\"noopener,noreferrer\");\n  }\n});\n</script></body></html>"
}
