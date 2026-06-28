package sopbackend

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func runAdminCLI(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sop-admin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", "", "Base URL of the SOP HTTP server")
	username := fs.String("username", "", "Username for HTTP auth")
	password := fs.String("password", "", "Password for HTTP auth")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: sop-admin [--server url] [--username user] [--password pass] <list-users|set-role|unregister>")
		return fmt.Errorf("missing subcommand")
	}

	subcommand := fs.Arg(0)
	if *serverURL == "" {
		return fmt.Errorf("server URL is required")
	}

	baseURL := strings.TrimRight(*serverURL, "/")
	switch subcommand {
	case "list-users":
		resp, err := adminHTTPPostJSON(baseURL+"/api/auth/login", map[string]string{"username": *username, "password": *password})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("login failed: %s", strings.TrimSpace(string(body)))
		}
		var loginResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
			return err
		}
		if loginResp.AccessToken == "" {
			return fmt.Errorf("login did not return an access token")
		}
		usersResp, err := adminHTTPRequest(baseURL+"/api/admin/users", http.MethodGet, loginResp.AccessToken, nil)
		if err != nil {
			return err
		}
		defer usersResp.Body.Close()
		if usersResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(usersResp.Body)
			return fmt.Errorf("list users failed: %s", strings.TrimSpace(string(body)))
		}
		var users []map[string]string
		if err := json.NewDecoder(usersResp.Body).Decode(&users); err != nil {
			return err
		}
		for _, user := range users {
			_, _ = fmt.Fprintf(stdout, "%s\t%s\t%s\n", user["username"], user["role"], user["status"])
		}
		return nil
	case "set-role":
		if fs.NArg() < 3 {
			_, _ = fmt.Fprintln(stderr, "usage: sop-admin --server url --username user --password pass set-role <username> <role>")
			return fmt.Errorf("missing role arguments")
		}
		resp, err := adminHTTPPostJSON(baseURL+"/api/auth/login", map[string]string{"username": *username, "password": *password})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("login failed: %s", strings.TrimSpace(string(body)))
		}
		var loginResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
			return err
		}
		if loginResp.AccessToken == "" {
			return fmt.Errorf("login did not return an access token")
		}
		roleResp, err := adminHTTPRequest(baseURL+"/api/auth/users/role", http.MethodPost, loginResp.AccessToken, map[string]string{"username": fs.Arg(1), "role": fs.Arg(2)})
		if err != nil {
			return err
		}
		defer roleResp.Body.Close()
		if roleResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(roleResp.Body)
			return fmt.Errorf("set role failed: %s", strings.TrimSpace(string(body)))
		}
		var rolePayload struct {
			User struct {
				Username string `json:"username"`
				Role     string `json:"role"`
			} `json:"user"`
		}
		if err := json.NewDecoder(roleResp.Body).Decode(&rolePayload); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "%s\t%s\n", rolePayload.User.Username, rolePayload.User.Role)
		return nil
	case "unregister":
		if fs.NArg() < 2 {
			_, _ = fmt.Fprintln(stderr, "usage: sop-admin --server url --username user --password pass unregister <username>")
			return fmt.Errorf("missing username")
		}
		resp, err := adminHTTPPostJSON(baseURL+"/api/auth/login", map[string]string{"username": *username, "password": *password})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("login failed: %s", strings.TrimSpace(string(body)))
		}
		var loginResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
			return err
		}
		if loginResp.AccessToken == "" {
			return fmt.Errorf("login did not return an access token")
		}
		unregisterResp, err := adminHTTPRequest(baseURL+"/api/auth/unregister", http.MethodPost, loginResp.AccessToken, nil)
		if err != nil {
			return err
		}
		defer unregisterResp.Body.Close()
		if unregisterResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(unregisterResp.Body)
			return fmt.Errorf("unregister failed: %s", strings.TrimSpace(string(body)))
		}
		_, _ = fmt.Fprintf(stdout, "removed\n")
		return nil
	default:
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n", subcommand)
		return fmt.Errorf("unknown subcommand %q", subcommand)
	}
}

func adminHTTPPostJSON(rawURL string, payload any) (*http.Response, error) {
	return adminHTTPRequest(rawURL, http.MethodPost, "", payload)
}

func adminHTTPRequest(rawURL, method, token string, payload any) (*http.Response, error) {
	var body []byte
	if payload != nil {
		switch v := payload.(type) {
		case []byte:
			body = v
		case string:
			body = []byte(v)
		default:
			var err error
			body, err = json.Marshal(payload)
			if err != nil {
				return nil, err
			}
		}
	}
	req, err := http.NewRequest(method, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

func handleAdminUsersPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isAdminRoleInContext(r.Context()) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	users := config.Users
	if len(users) == 0 {
		if store := config.ensureUserStore(); store != nil {
			if storedUsers, err := store.List(r.Context()); err == nil {
				users = make([]UserRecord, 0, len(storedUsers))
				for _, u := range storedUsers {
					users = append(users, u)
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Admin Users</title></head><body>")
	b.WriteString("<h1>Admin Users</h1>")
	b.WriteString("<table><tr><th>Username</th><th>Role</th><th>Status</th></tr>")
	for _, user := range users {
		b.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td></tr>", htmlEscape(user.Username), htmlEscape(user.Role), htmlEscape(user.Status)))
	}
	b.WriteString("</table></body></html>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func handleAdminUsersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isAdminRoleInContext(r.Context()) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	users := config.Users
	if len(users) == 0 {
		if store := config.ensureUserStore(); store != nil {
			if storedUsers, err := store.List(r.Context()); err == nil {
				users = make([]UserRecord, 0, len(storedUsers))
				for _, u := range storedUsers {
					users = append(users, u)
				}
			}
		}
	}

	payload := make([]map[string]string, 0, len(users))
	for _, user := range users {
		payload = append(payload, map[string]string{
			"username": user.Username,
			"role":     user.Role,
			"status":   user.Status,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func htmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	return strings.ReplaceAll(value, "'", "&#39;")
}

// RunAdminCLI exposes the admin CLI entrypoint for the standalone sop-admin command.
func RunAdminCLI(args []string, stdout, stderr io.Writer) error {
	return runAdminCLI(args, stdout, stderr)
}
