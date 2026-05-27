package sms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Client gère la connexion au modem TCL LinkKey IK41 via HTTP
type Client struct {
	http        *http.Client
	jar         *cookiejar.Jar
	baseURL     string
	modemURL    *url.URL
	verifKey    string
	freeKey     string
	hmacKey     string
	xorKey      string
	loginToken  string
	password    string
	whitelist   []string
	sessionKey  string // clé de chiffrement post-login (formatStr(pbkdf2password))
	derniersLus map[string]bool
}

// SMS représente un message reçu
type SMS struct {
	Numero  string
	Message string
}

// New crée un client et se connecte au modem
func New(baseURL, password, verifKey, xorKey, freeKey, hmacKey, whitelist string) (*Client, error) {
	jar, _ := cookiejar.New(nil)
	modemURL, _ := url.Parse(baseURL)

	c := &Client{
		http:        &http.Client{Timeout: 10 * time.Second, Jar: jar},
		jar:         jar,
		baseURL:     baseURL + "/jrd/webapi",
		modemURL:    modemURL,
		xorKey:      xorKey,
		verifKey:    verifKey,
		freeKey:     freeKey,
		hmacKey:     hmacKey,
		whitelist:   strings.Split(whitelist, ","),
		derniersLus: make(map[string]bool),
		password:    password,
	}

	if err := c.login(); err != nil {
		return nil, fmt.Errorf("connexion modem TCL : %w", err)
	}
	return c, nil
}

// ---- Authentification ----

func (c *Client) login() error {
	// Initialiser le cookie à "null" pour le premier appel
	c.setToken("null")

	// 1. Récupérer le Salt
	deviceSt, err := c.call("GetDeviceSt", nil)
	if err != nil {
		return fmt.Errorf("GetDeviceSt : %w", err)
	}
	salt, _ := deviceSt["Salt"].(string)

	// 2. Préparer les credentials
	pwHash := pbkdf2Password(c.password, salt)
	params := map[string]interface{}{
		"UserName": xorEncrypt("admin", c.xorKey),
		"Password": pwHash,
	}

	// 3. Login — ForceLogin si session existante
	result, err := c.call("Login", params)
	if err != nil {
		result, err = c.call("ForceLogin", params)
		if err != nil {
			return fmt.Errorf("Login/ForceLogin : %w", err)
		}
	}

	token, ok := result["token"].(string)
	if !ok || token == "" {
		return fmt.Errorf("token absent de la réponse : %v", result)
	}

	c.setToken(token)

	// Clé de session = formatStr(pbkdf2password) — utilisée pour les APIs non-free
	// formatStr = swap des deux moitiés de la string
	c.sessionKey = formatStr(pwHash)
	return nil
}

// setToken met à jour le token dans le client et le cookie jar
func (c *Client) setToken(token string) {
	c.loginToken = token
	c.jar.SetCookies(c.modemURL, []*http.Cookie{
		{Name: "loginToken", Value: token, Path: "/"},
	})
}

// ---- SMS ----

// EnvoyerSMS envoie un SMS au numéro donné
func (c *Client) EnvoyerSMS(numero, message string) error {
	const maxRetries = 3

	var sendErr error
	for attempt := range maxRetries {
		_, sendErr = c.call("SendSMS", map[string]interface{}{
			"SMSId":       -1,
			"SMSContent":  message,
			"PhoneNumber": numero,
			"SMSTime":     time.Now().Format("2006-01-02 15:04:05"),
		})
		if sendErr == nil {
			break
		}
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}
	if sendErr != nil {
		return fmt.Errorf("SendSMS après %d tentatives : %w", maxRetries, sendErr)
	}

	// Attendre confirmation d'envoi (max 10 secondes)
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		result, err := c.call("GetSendSMSResult", nil)
		if err != nil {
			break
		}
		status, _ := result["SendStatus"].(float64)
		if status == 2 { // SMS_SEND_STATUS_SUCCESS
			break
		}
		if status == 5 { // SMS_SEND_STATUS_FAILED
			return fmt.Errorf("envoi SMS échoué")
		}
	}
	log.Printf("📤 SMS envoyé à %s : %s", numero, message)
	return nil
}

func (c *Client) EcouterSMS(canal chan<- SMS) {
	for {
		idsActuels := map[string]bool{}

		time.Sleep(10 * time.Second)

		contacts, err := c.getSMSContacts()
		if err != nil {
			if strings.Contains(err.Error(), "-32699") || strings.Contains(err.Error(), "-32697") {
				if loginErr := c.login(); loginErr != nil {
					fmt.Println(loginErr)
				}
			}
			continue
		}

		for _, contact := range contacts {
			contactID, _ := contact["ContactId"].(float64)
			messages, err := c.getSMSContent(int(contactID))
			if err != nil {
				continue
			}
			for _, msg := range messages {
				smsID, _ := msg["SMSId"].(float64)
				smsType, _ := contact["SMSType"].(float64)
				key := fmt.Sprintf("%.0f", smsID)
				idsActuels[key] = true

				if c.derniersLus[key] {
					continue
				}
				c.derniersLus[key] = true

				var numero string
				switch v := contact["PhoneNumber"].(type) {
				case []interface{}:
					if len(v) > 0 {
						numero, _ = v[0].(string)
					}
				case string:
					numero = v
				}

				numeroConnu := slices.Contains(c.whitelist, numero)
				contenu, _ := msg["SMSContent"].(string)

				if smsType != 2 && numeroConnu {
					if contenu != "" {
						log.Printf("📱 SMS reçu de %s : %s", numero, contenu)
						canal <- SMS{
							Numero:  numero,
							Message: strings.ToLower(contenu),
						}
					}
				}

				if !numeroConnu {
					log.Printf("📱 SMS reçu d'un numéro inconnu %s : %s", numero, contenu)
				}

				// Supprimer le SMS après traitement
				_, err := c.call("DeleteSMS", map[string]interface{}{
					"DelFlag":   2, // SMS_DELETE_FLAG_Content
					"ContactId": int(contactID),
					"SMSId":     int(smsID),
				})
				if err != nil {
					fmt.Println(err)
				}

			}
		}

		for key := range c.derniersLus {
			if !idsActuels[key] {
				delete(c.derniersLus, key)
			}
		}
	}
}

// ---- Helpers API ----

func (c *Client) getSMSContacts() ([]map[string]interface{}, error) {
	result, err := c.call("GetSMSContactList", map[string]interface{}{"Page": 1})
	if err != nil {
		return nil, err
	}
	list, _ := result["SMSContactList"].([]interface{})
	var contacts []map[string]interface{}
	for _, item := range list {

		if m, ok := item.(map[string]interface{}); ok {
			contacts = append(contacts, m)
		}
	}
	return contacts, nil
}

func (c *Client) getSMSContent(contactID int) ([]map[string]interface{}, error) {
	result, err := c.call("GetSMSContentList", map[string]interface{}{
		"Page":      1,
		"ContactId": contactID,
	})
	if err != nil {
		return nil, err
	}
	list, _ := result["SMSContentList"].([]interface{})
	var messages []map[string]interface{}
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			messages = append(messages, m)
		}
	}
	return messages, nil
}

// freeAPIs = config.freeApis dans config.js — chiffrées avec freeApiKey
var freeAPIs = map[string]bool{
	"GetCurrentLanguage": true, "SetLanguage": true, "Login": true,
	"ForceLogin": true, "Logout": true, "GetAutoValidatePinState": true,
	"GetConnectionSettings": true, "GetConnectionState": true,
	"GetDeviceUpgradeState": true, "GetLoginState": true,
	"GetNetworkInfo": true, "GetSMSStorageState": true,
	"GetSimStatus": true, "GetSystemStatus": true,
	"SetCheckNewVersion": true, "getSmsInitState": true,
	"GetSystemInfo": true, "GetDeviceSt": true,
	"SetPassWordReset": true, "GetDeviceIdentity": true,
}

// formatStr = swap des deux moitiés de la string (crypto_utils.formatStr dans sdk.js)
func formatStr(s string) string {
	n := len(s)
	half := n / 2
	return s[half:] + s[:half]
}

// methodIDs mappe chaque méthode à son id JSON-RPC (défini dans sdk.js)
var methodIDs = map[string]string{
	"Login":              "1.1",
	"ForceLogin":         "1.6",
	"Logout":             "1.2",
	"GetLoginState":      "1.3",
	"HeartBeat":          "1.5",
	"GetDeviceSt":        "1.1",
	"GetSimStatus":       "2.1",
	"GetConnectionState": "3.1",
	"GetNetworkInfo":     "4.1",
	"GetSMSContactList":  "6.2",
	"GetSMSContentList":  "6.3",
	"GetSMSStorageState": "6.4",
	"SendSMS":            "6.6",
	"DeleteSMS":          "6.5",
	"GetSMSSettings":     "6.9",
	"GetSystemInfo":      "13.1",
	"GetSystemStatus":    "13.4",
}

func methodID(method string) string {
	if id, ok := methodIDs[method]; ok {
		return id
	}
	return "1.1"
}

// encKey get key
func (c *Client) encKey(method string) string {
	if freeAPIs[method] || c.sessionKey == "" {
		return c.freeKey
	}
	return c.sessionKey
}

func (c *Client) call(method string, params map[string]interface{}) (map[string]interface{}, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	params["_"] = time.Now().UnixMilli()

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	// Choisir la clé selon si l'API est "free" ou non (comme dans apiPost du sdk.js)
	encKey := c.encKey(method)

	encParams, err := aesEncrypt(string(paramsJSON), encKey)
	if err != nil {
		return nil, fmt.Errorf("chiffrement : %w", err)
	}

	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  encParams,
		"id":      methodID(method),
		"hmac":    computeHmac(string(paramsJSON), c.hmacKey),
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.baseURL+"?api="+method, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	baseHost := strings.TrimSuffix(c.baseURL, "/jrd/webapi")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", baseHost+"/index.html")
	req.Header.Set("Origin", baseHost)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header["_TclRequestVerificationKey"] = []string{c.verifKey}
	req.Header["_TclRequestVerificationToken"] = []string{c.loginToken}
	req.Header["aVer"] = []string{"2.0"}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Result interface{} `json:"result"`
		Error  interface{} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("réponse invalide : %w", err)
	}

	if data.Error != nil {
		return nil, fmt.Errorf("erreur API : %v", data.Error)
	}

	switch v := data.Result.(type) {
	case string:
		decKey := c.encKey(method)
		decrypted, err := aesDecrypt(v, decKey)
		if err != nil {
			return map[string]interface{}{"raw": v}, nil
		}
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(decrypted), &result); err != nil {
			return map[string]interface{}{"value": decrypted}, nil
		}
		return result, nil
	case map[string]interface{}:
		return v, nil
	default:
		return map[string]interface{}{}, nil
	}
}
