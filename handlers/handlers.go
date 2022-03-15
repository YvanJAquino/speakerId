package handlers

import (
	// dfcx "github.com/YvanJAquino/dfcx-sfdc-oauth2/dfcx"

	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/YvanJAquino/dfcx-sfdc-oauth2/dfcx"
	farm "github.com/dgryski/go-farm"
)

var (
	// SelectAccountIdQuery = "SELECT accountId FROM phones WHERE phoneNumber = {ph}"
	// SelectGcpResources = "SELECT gcpResourceName FROM speakerIds WHERE accountId IN ({ans})"
	// parent = context.Background()
	// db     = "projects/vocal-etching-343420/instances/speaker-id/databases/speaker-id"
	GetAccountsByPhoneNumberPrototype = `
SELECT  *,
ARRAY(SELECT AS STRUCT * FROM speakerIds WHERE speakerIds.accountId = phones.accountId) AS speakerIds,
ARRAY(SELECT AS STRUCT * FROM accounts WHERE accounts.accountId = phones.accountId and accounts.accountName = "testingApp") AS accounts
FROM    phones
Where   phones.phoneNumber = "{ph}"`
	InsertNewSpeakerIds = `
INSERT INTO speakerIds
		(gcpResourceName, accountId, speakerId)
VALUES	("{grn}", {ac}, {sid})`
)

type Account struct {
	AccountId   int64        `spanner:"accountId"`
	AccountName string       `spanner:"accountName"`
	Pin         string       `spanner:"pin"`
	Phones      []*Phone     `spanner:"phones,omitempty"`
	SpeakerIds  []*SpeakerId `spanner:"speakerIds,omitempty"`
}

type Phone struct {
	PhoneId     int64        `spanner:"phoneId"`
	AccountId   int64        `spanner:"accountId"`
	PhoneNumber string       `spanner:"phoneNumber"`
	Accounts    []*Account   `spanner:"accounts,omitempty"`
	SpeakerIds  []*SpeakerId `spanner:"speakerIds,omitempty"`
}

type SpeakerId struct {
	SpeakerId       int64      `spanner:"speakerId"`
	AccountId       int64      `spanner:"accountId"`
	GCPResourceName string     `spanner:"gcpResourceName"`
	Accounts        []*Account `spanner:"accounts,omitempty"`
	Phones          []*Phone   `spanner:"phones,omitempty"`
}

type SpeakerIdHandler struct {
	Client *spanner.Client
}

func (h *SpeakerIdHandler) Using(c *spanner.Client) *SpeakerIdHandler {
	return &SpeakerIdHandler{Client: c}
}

func (h *SpeakerIdHandler) GenerateId(data ...[]byte) int64 {
	inp := make([]byte, 0)
	for _, d := range data {
		inp = append(inp, d...)
	}
	return int64(farm.Hash32(inp))
}

func (h *SpeakerIdHandler) GetAccountsByPhoneNumber(ctx context.Context, ph string) []*Phone {
	var data []*Phone
	query := strings.Replace(GetAccountsByPhoneNumberPrototype, "{ph}", ph, -1)
	stmt := spanner.Statement{SQL: query}
	rows := h.Client.Single().Query(ctx, stmt)
	defer rows.Stop()
	for {
		row, err := rows.Next()
		if err != nil {
			fmt.Println("There's an error, Jim!", err)
			break
		}
		p := &Phone{}
		row.ToStruct(p)
		data = append(data, p)
	}
	return data
}

func (h *SpeakerIdHandler) RegisterNewSpeakerId(ctx context.Context, speakerId string, accountId int64) error {
	query := strings.Replace(InsertNewSpeakerIds, "{grn}", speakerId, -1)
	query = strings.Replace(query, "{ac}", strconv.FormatInt(accountId, 10), -1)
	newId := h.GenerateId([]byte(strconv.FormatInt(accountId, 10)), []byte(speakerId))
	query = strings.Replace(query, "{sid}", strconv.FormatInt(newId, 10), -1)
	stmt := spanner.Statement{SQL: query}
	_, err := h.Client.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		_, err := txn.Update(ctx, stmt)
		if err != nil {
			return err
		}
		fmt.Println("A record was written.")
		return nil
	})
	return err
}

func (h *SpeakerIdHandler) GetSpeakerIdsHandler(w http.ResponseWriter, r *http.Request) {
	wh, err := dfcx.FromRequest(r)
	if err != nil {
		fmt.Println(err)
		return
	}
	telephony, _ := wh.Payload["telephony"].(map[string]string)
	callerId := telephony["caller_id"]
	phoneData := h.GetAccountsByPhoneNumber(r.Context(), callerId)
	gcpResourceNames := make([]string, 0)
	for _, data := range phoneData {
		for _, speakerIds := range data.SpeakerIds {
			gcpResourceNames = append(gcpResourceNames, speakerIds.GCPResourceName)
		}
	}
	if len(gcpResourceNames) == 0 {
		return
	} else {
		params := make(map[string]string)
		params["speakerIds"] = strings.Join(gcpResourceNames, ", ")
		resp := &dfcx.WebhookResponse{
			SessionInfo: &dfcx.SessionInfo{
				Parameters: params,
			},
		}
		resp.Respond(w)
	}
}

func (h *SpeakerIdHandler) RegisterSpeakerIdsHandler(w http.ResponseWriter, r *http.Request) {
	wh, err := dfcx.FromRequest(r)
	if err != nil {
		fmt.Println(err)
		return
	}
	telephony, _ := wh.Payload["telephony"].(map[string]string)
	callerId := telephony["caller_id"]
	phoneData := h.GetAccountsByPhoneNumber(r.Context(), callerId)
	accountId := phoneData[0].AccountId
	newSpeakerId := wh.SessionInfo.Parameters["new-speaker-id"]
	err = h.RegisterNewSpeakerId(r.Context(), newSpeakerId, accountId)
	if err != nil {
		fmt.Println(err)
		return
	}
	resp := &dfcx.WebhookResponse{
		SessionInfo: &dfcx.SessionInfo{
			Parameters: map[string]string{
				"speakerIdRegistered": "true",
				"userAuthenticated":   "true",
			},
		},
	}
	resp.Respond(w)
}

func (h *SpeakerIdHandler) VerifyPinNumber(w http.ResponseWriter, r *http.Request) {
	wh, err := dfcx.FromRequest(r)
	if err != nil {
		fmt.Println(err)
		return
	}
	// Get the phone number
	telephony, ok := wh.Payload["telephony"].(map[string]string)
	if !ok {
		fmt.Println("Assertion failed: ", telephony)
	}
	ph := telephony["caller_id"]

	// Get the provided pin
	pinVal := wh.PageInfo.FormInfo.ParameterInfo[0].Value
	userPin, ok := pinVal.(string)
	if !ok {
		fmt.Println("Not a string")
		return
	}
	phData := h.GetAccountsByPhoneNumber(r.Context(), ph)
	acctPin := phData[0].Accounts[0].Pin
	params := make(map[string]string)
	params["userAuthenticated"] = strconv.FormatBool(acctPin == userPin)
	resp := &dfcx.WebhookResponse{
		SessionInfo: &dfcx.SessionInfo{
			Parameters: params,
		},
	}
	resp.Respond(w)
}

// func (h *SpeakerIdHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
// 	wh, err := dfcx.FromRequest(r)
// 	if err != nil {
// 		fmt.Println(err)
// 		return
// 	}
// 	telephony, ok := wh.Payload["telephony"].(map[string]string)
// 	if !ok {
// 		fmt.Println("AssertionError")
// 	}
// 	callerId := telephony["caller_id"]

// 	accountIds := make([]string, 0)
// 	stmt := spanner.Statement{SQL: strings.Replace(SelectAccountIdQuery, "{ph}", "+1"+callerId, -1)}
// 	accountRows := h.Client.Single().Query(r.Context(), stmt)
// 	defer accountRows.Stop()
// 	for {
// 		row, err := accountRows.Next()
// 		if err != nil {
// 			fmt.Println(err)
// 			break
// 		}
// 		var accountId int
// 		err = row.Column(0, &accountId)
// 		if err != nil {
// 			fmt.Println(err)
// 			break
// 		}
// 		acctId := strconv.Itoa(accountId)
// 		accountIds = append(accountIds, acctId)
// 	}

// 	var gcpResources []string
// 	allAcctIds := strings.Join(accountIds, ", ")
// 	stmt = spanner.Statement{SQL: strings.Replace(SelectGcpResources, "{ans}", allAcctIds, -1)}
// 	speakerIdRows := h.Client.Single().Query(r.Context(), stmt)
// 	defer speakerIdRows.Stop()
// 	for {
// 		row, err := speakerIdRows.Next()
// 		if err != nil {
// 			fmt.Println(err)
// 			break
// 		}
// 		var gcpResource string
// 		err = row.Column(0, &gcpResource)
// 		if err != nil {
// 			fmt.Println(err)
// 			break
// 		}
// 		gcpResources = append(gcpResources, gcpResource)
// 	}

// 	if len(gcpResources) == 0 {
// 		fmt.Fprint(w, "No Resources (GcpResourceName) Found...")
// 		return
// 	} else {
// 		params := map[string]string{
// 			"speaker-ids": strings.Join(gcpResources, ", "),
// 		}
// 		resp := &dfcx.WebhookResponse{
// 			SessionInfo: &dfcx.SessionInfo{
// 				Parameters: params,
// 			},
// 		}
// 		json.NewEncoder(w).Encode(resp)
// 	}

// }
