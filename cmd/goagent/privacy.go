package main

import (
	"fmt"
	"net/http"
)

func configPrivacyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "method not allowed"})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, genericPrivacyStatement())
	}
}

func genericPrivacyStatement() string {
	return "GoAgent Privacy Policy\n\n" +
		"GoAgent is a locally run helper service operated by the user who installed it. This service lets a custom GPT call local GoAgent endpoints selected and configured by that user.\n\n" +
		"Information processed\n" +
		"GoAgent may process request details sent to its configured endpoints, including query parameters, request paths, and data needed by the selected local helper endpoint. GoAgent may also serve user-provided knowledge files from the local knowledge directory when the correct API key is supplied.\n\n" +
		"Storage\n" +
		"GoAgent stores its configuration, API keys, provider configuration, cached files, and optional knowledge files on the user's local machine under the GoAgent state directory. GoAgent does not provide a hosted cloud database.\n\n" +
		"Sharing\n" +
		"GoAgent does not intentionally sell, rent, or share user data with third parties. Data may pass through services chosen by the user to expose the local service, such as a tunnel provider, and through ChatGPT Actions when the user configures a GPT to call GoAgent.\n\n" +
		"User control\n" +
		"The user controls which endpoints are configured, which files are placed in the knowledge directory, which API key is used, and whether the local service or tunnel is running. The user may stop GoAgent, rotate API keys, remove knowledge files, or delete the local GoAgent state directory at any time.\n\n" +
		"Security\n" +
		"Protected GoAgent endpoints require the configured X-API-Key header. The schema and privacy endpoints are public so that GPT configuration tools can read them.\n\n" +
		"Contact\n" +
		"For privacy questions, contact the person or organisation operating this GoAgent instance.\n"
}
