package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var playerActionTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.:@-]{1,128}$`)

type restHTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *restHTTPError) Error() string {
	return fmt.Sprintf("REST %s: %s", e.Status, e.Body)
}

func restGet(instance ServerInstance, endpoint string) (map[string]any, error) {
	return restRequest(instance, http.MethodGet, endpoint, nil)
}

func restPost(instance ServerInstance, endpoint string, body any) (map[string]any, error) {
	return restRequest(instance, http.MethodPost, endpoint, body)
}

func restRequest(instance ServerInstance, method, endpoint string, body any) (map[string]any, error) {
	data, err := restRequestBytes(instance, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("decode REST response: %w", err)
		}
	}
	return result, nil
}

func restRequestBytes(instance ServerInstance, method, endpoint string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d/v1/api%s", instance.RESTPort, endpoint), reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("admin", instance.AdminPassword)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &restHTTPError{StatusCode: resp.StatusCode, Status: resp.Status, Body: strings.TrimSpace(string(data))}
	}
	return data, nil
}

func restInfo(instance ServerInstance) (map[string]any, error) { return restGet(instance, "/info") }

func rconPacket(id, packetType int32, payload string) []byte {
	body := make([]byte, 10+len(payload))
	binary.LittleEndian.PutUint32(body[0:4], uint32(id))
	binary.LittleEndian.PutUint32(body[4:8], uint32(packetType))
	copy(body[8:], payload)
	packet := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(packet[:4], uint32(len(body)))
	copy(packet[4:], body)
	return packet
}

func readRCONPacket(conn net.Conn) (int32, int32, string, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(conn, lengthBytes); err != nil {
		return 0, 0, "", err
	}
	length := int(binary.LittleEndian.Uint32(lengthBytes))
	if length < 10 || length > 4*1024*1024 {
		return 0, 0, "", errors.New("invalid RCON packet length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return 0, 0, "", err
	}
	id := int32(binary.LittleEndian.Uint32(body[0:4]))
	typ := int32(binary.LittleEndian.Uint32(body[4:8]))
	payload := strings.TrimRight(string(body[8:length-2]), "\x00")
	return id, typ, payload, nil
}

func openAuthenticatedRCON(instance ServerInstance, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialTimeout := min(timeout, 3*time.Second)
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", instance.RCONPort), dialTimeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(rconPacket(1, 3, instance.AdminPassword)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	authenticated := false
	for i := 0; i < 2; i++ {
		id, _, _, readErr := readRCONPacket(conn)
		if readErr != nil {
			_ = conn.Close()
			return nil, readErr
		}
		if id == -1 {
			_ = conn.Close()
			return nil, errors.New("RCON authentication failed")
		}
		if id == 1 {
			authenticated = true
			break
		}
	}
	if !authenticated {
		_ = conn.Close()
		return nil, errors.New("RCON authentication response missing")
	}
	return conn, nil
}

func probeRCONWithTimeout(instance ServerInstance, timeout time.Duration) error {
	conn, err := openAuthenticatedRCON(instance, timeout)
	if err != nil {
		return err
	}
	return conn.Close()
}

func probeRCON(instance ServerInstance) error {
	return probeRCONWithTimeout(instance, 3*time.Second)
}

func sendRCONWithTimeout(instance ServerInstance, command string, timeout time.Duration) (string, error) {
	conn, err := openAuthenticatedRCON(instance, timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(rconPacket(2, 2, command)); err != nil {
		return "", err
	}
	var result strings.Builder
	for {
		id, _, payload, readErr := readRCONPacket(conn)
		if readErr != nil {
			if result.Len() > 0 {
				break
			}
			if timeoutErr, ok := readErr.(net.Error); ok && timeoutErr.Timeout() {
				// Palworld 1.0 executes some native RCON commands without sending a
				// response packet. Authentication and a successful command write are
				// therefore sufficient for fire-and-forget commands.
				return "", nil
			}
			return "", readErr
		}
		if id == 2 {
			result.WriteString(payload)
			break
		}
	}
	return strings.TrimSpace(result.String()), nil
}

func sendRCON(instance ServerInstance, command string) (string, error) {
	return sendRCONWithTimeout(instance, command, 5*time.Second)
}

func (a *App) SendRCON(id, command string) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	return sendRCON(instance, command)
}

func (a *App) GetPlayers(id string) ([]Player, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	return getOfficialPlayers(instance)
}

func (a *App) PlayerAction(id string, request ActionRequest) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	if err := validatePlayerActionToken("userId", request.UserID); err != nil {
		return "", err
	}
	switch request.Action {
	case "kick", "ban":
		_, err = restPost(instance, "/"+request.Action, map[string]any{"userid": request.UserID, "message": "Managed by Palserver Launcher"})
		return "OK", err
	}
	commands, err := buildPlayerActionCommands(request)
	if err != nil {
		return "", err
	}
	responses := make([]string, 0, len(commands))
	for _, command := range commands {
		response, actionErr := sendRCON(instance, command)
		if actionErr != nil {
			return "", actionErr
		}
		responses = append(responses, response)
	}
	return strings.Join(responses, "\n"), nil
}

func validatePlayerActionToken(label, value string) error {
	if !playerActionTokenPattern.MatchString(value) {
		return fmt.Errorf("%s contains unsupported characters", label)
	}
	return nil
}

func buildPlayerActionCommands(request ActionRequest) ([]string, error) {
	if err := validatePlayerActionToken("userId", request.UserID); err != nil {
		return nil, err
	}
	requireValue := func(label, value string) error { return validatePlayerActionToken(label, value) }
	amount := max(1, request.Amount)
	switch request.Action {
	case "setadmin":
		return []string{"setadmin " + request.UserID}, nil
	case "ipban":
		return []string{"ipban " + request.UserID}, nil
	case "item":
		if err := requireValue("itemId", request.Value); err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("give %s %s %d", request.UserID, request.Value, amount)}, nil
	case "exp":
		return []string{fmt.Sprintf("give_exp %s %d", request.UserID, amount)}, nil
	case "relic":
		return []string{fmt.Sprintf("give_relic %s CapturePower %d", request.UserID, amount)}, nil
	case "tech":
		return []string{fmt.Sprintf("givetechpoints %s %d", request.UserID, amount)}, nil
	case "bosstech":
		return []string{fmt.Sprintf("givebosstechpoints %s %d", request.UserID, amount)}, nil
	case "stats":
		return []string{fmt.Sprintf("givestats %s %d", request.UserID, amount)}, nil
	case "learntech":
		if err := requireValue("techId", request.Value); err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("learntech %s %s", request.UserID, request.Value)}, nil
	case "egg":
		if err := requireValue("eggId", request.Value); err != nil {
			return nil, err
		}
		if err := requireValue("palId", request.Extra); err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("giveegg %s %s %s %d", request.UserID, request.Value, request.Extra, amount)}, nil
	case "pal":
		if err := requireValue("palId", request.Value); err != nil {
			return nil, err
		}
		commands := make([]string, amount)
		for index := range commands {
			commands[index] = fmt.Sprintf("givepal %s %s", request.UserID, request.Value)
		}
		return commands, nil
	default:
		return nil, errors.New("unsupported player action")
	}
}
