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
	"strings"
	"time"
)

func restGet(instance ServerInstance, endpoint string) (map[string]any, error) {
	return restRequest(instance, http.MethodGet, endpoint, nil)
}

func restPost(instance ServerInstance, endpoint string, body any) (map[string]any, error) {
	return restRequest(instance, http.MethodPost, endpoint, body)
}

func restRequest(instance ServerInstance, method, endpoint string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
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
		return nil, fmt.Errorf("REST %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	result := map[string]any{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &result)
	}
	return result, nil
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

func sendRCON(instance ServerInstance, command string) (string, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", instance.RCONPort), 3*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(rconPacket(1, 3, instance.AdminPassword)); err != nil {
		return "", err
	}
	authenticated := false
	for i := 0; i < 2; i++ {
		id, _, _, readErr := readRCONPacket(conn)
		if readErr != nil {
			return "", readErr
		}
		if id == -1 {
			return "", errors.New("RCON authentication failed")
		}
		if id == 1 {
			authenticated = true
			break
		}
	}
	if !authenticated {
		return "", errors.New("RCON authentication response missing")
	}
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
			return "", readErr
		}
		if id == 2 {
			result.WriteString(payload)
			break
		}
	}
	return strings.TrimSpace(result.String()), nil
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
	result, err := restGet(instance, "/players")
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(result["players"])
	var payload []struct {
		Name, AccountName, PlayerID, UserID, IP, IPAlt string
		Ping, LocationX, LocationY                     float64
		Level                                          int
	}
	var generic []map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	players := make([]Player, 0, len(generic))
	for _, item := range generic {
		ip := fmt.Sprint(item["ip"])
		if ip == "<nil>" || ip == "" {
			ip = fmt.Sprint(item["iP"])
		}
		players = append(players, Player{
			Name: fmt.Sprint(item["name"]), AccountName: fmt.Sprint(item["accountName"]),
			PlayerID: fmt.Sprint(item["playerId"]), UserID: fmt.Sprint(item["userId"]), IP: ip,
			Ping: number(item["ping"]), LocationX: number(item["location_x"]), LocationY: number(item["location_y"]), Level: int(number(item["level"])),
		})
	}
	_ = payload
	return players, nil
}

func (a *App) PlayerAction(id string, request ActionRequest) (string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return "", err
	}
	if request.UserID == "" {
		return "", errors.New("userId is required")
	}
	switch request.Action {
	case "kick", "ban":
		_, err = restPost(instance, "/"+request.Action, map[string]any{"userid": request.UserID, "message": "Managed by Palserver Launcher"})
		return "OK", err
	case "setadmin":
		return sendRCON(instance, "setadmin "+request.UserID)
	case "ipban":
		return sendRCON(instance, "ipban "+request.UserID)
	case "item":
		return sendRCON(instance, fmt.Sprintf("give %s %s %d", request.UserID, request.Value, max(1, request.Amount)))
	case "exp":
		return sendRCON(instance, fmt.Sprintf("give_exp %s %d", request.UserID, max(1, request.Amount)))
	case "relic":
		return sendRCON(instance, fmt.Sprintf("give_relic %s CapturePower %d", request.UserID, max(1, request.Amount)))
	case "tech":
		return sendRCON(instance, fmt.Sprintf("givetechpoints %s %d", request.UserID, max(1, request.Amount)))
	case "bosstech":
		return sendRCON(instance, fmt.Sprintf("givebosstechpoints %s %d", request.UserID, max(1, request.Amount)))
	case "pal":
		count := max(1, request.Amount)
		responses := make([]string, 0, count)
		for i := 0; i < count; i++ {
			response, actionErr := sendRCON(instance, fmt.Sprintf("givepal %s %s", request.UserID, request.Value))
			if actionErr != nil {
				return "", actionErr
			}
			responses = append(responses, response)
		}
		return strings.Join(responses, "\n"), nil
	default:
		return "", errors.New("unsupported player action")
	}
}
