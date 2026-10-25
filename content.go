package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// Altri tipi di contenuto, oltre a quelli in store.go.
const (
	KLocation = "location"
	KContact  = "contact"
	KPoll     = "poll"
	KCall     = "call"
	KEvent    = "event"
)

// MsgExtra contiene i dettagli che servono a disegnare i contenuti speciali.
type MsgExtra struct {
	Title     string   `json:"t,omitempty"` // titolo anteprima link, nome luogo, domanda sondaggio
	Desc      string   `json:"d,omitempty"`
	URL       string   `json:"u,omitempty"`
	Options   []string `json:"o,omitempty"` // opzioni del sondaggio, numeri del contatto
	Forwarded bool     `json:"f,omitempty"`
	GIF       bool     `json:"g,omitempty"`
	ViewOnce  bool     `json:"v,omitempty"`
	Size      uint64   `json:"s,omitempty"`
	Pages     uint32   `json:"p,omitempty"`
	Seconds   uint32   `json:"sec,omitempty"`
	W         uint32   `json:"w,omitempty"`
	H         uint32   `json:"h,omitempty"`
	Animated  bool     `json:"a,omitempty"`
}

func (e MsgExtra) encode() string {
	b, _ := json.Marshal(e)
	if string(b) == "{}" {
		return ""
	}
	return string(b)
}

func decodeExtra(s string) (e MsgExtra) {
	if s != "" {
		json.Unmarshal([]byte(s), &e)
	}
	return
}

type parsed struct {
	Kind, Text, FileName string
	Thumb                []byte
	Media                bool
	Extra                MsgExtra
	QuoteID              string
	QuoteSender          string
	QuoteText            string
}

func contextOf(m *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case m.ExtendedTextMessage != nil:
		return m.GetExtendedTextMessage().GetContextInfo()
	case m.ImageMessage != nil:
		return m.GetImageMessage().GetContextInfo()
	case m.VideoMessage != nil:
		return m.GetVideoMessage().GetContextInfo()
	case m.PtvMessage != nil:
		return m.GetPtvMessage().GetContextInfo()
	case m.AudioMessage != nil:
		return m.GetAudioMessage().GetContextInfo()
	case m.DocumentMessage != nil:
		return m.GetDocumentMessage().GetContextInfo()
	case m.StickerMessage != nil:
		return m.GetStickerMessage().GetContextInfo()
	case m.LocationMessage != nil:
		return m.GetLocationMessage().GetContextInfo()
	case m.LiveLocationMessage != nil:
		return m.GetLiveLocationMessage().GetContextInfo()
	case m.ContactMessage != nil:
		return m.GetContactMessage().GetContextInfo()
	case m.ContactsArrayMessage != nil:
		return m.GetContactsArrayMessage().GetContextInfo()
	case m.PollCreationMessageV3 != nil:
		return m.GetPollCreationMessageV3().GetContextInfo()
	case m.PollCreationMessage != nil:
		return m.GetPollCreationMessage().GetContextInfo()
	}
	return nil
}

var reURL = regexp.MustCompile(`(?i)\b(https?://[^\s<>"]+|www\.[^\s<>"]+)`)

func firstURL(s string) string {
	u := reURL.FindString(s)
	u = strings.TrimRight(u, ".,;:!?)»”'")
	if u != "" && !strings.HasPrefix(strings.ToLower(u), "http") {
		u = "https://" + u
	}
	return u
}

var reTel = regexp.MustCompile(`(?m)^TEL[^:]*:(.+)$`)

func phonesOf(vcard string) []string {
	var out []string
	for _, m := range reTel.FindAllStringSubmatch(vcard, 4) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// parseContent trasforma un messaggio WhatsApp in qualcosa di disegnabile.
// Kind vuoto = messaggio senza contenuto visibile (va ignorato).
func parseContent(m *waE2E.Message) (p parsed) {
	if m == nil {
		return
	}
	if ci := contextOf(m); ci != nil {
		p.Extra.Forwarded = ci.GetIsForwarded()
		if q := ci.GetQuotedMessage(); q != nil {
			qp := parseContent(q)
			p.QuoteID = ci.GetStanzaID()
			p.QuoteSender = ci.GetParticipant()
			p.QuoteText = previewOf(qp.Kind, qp.Text, qp.FileName)
			if p.QuoteText == "" {
				p.QuoteText = "Messaggio"
			}
		}
	}
	switch {
	case m.Conversation != nil:
		p.Kind, p.Text = KText, m.GetConversation()
	case m.ExtendedTextMessage != nil:
		x := m.GetExtendedTextMessage()
		p.Kind, p.Text = KText, x.GetText()
		if x.GetTitle() != "" || x.GetMatchedText() != "" {
			p.Extra.Title, p.Extra.Desc = x.GetTitle(), x.GetDescription()
			p.Extra.URL = x.GetMatchedText()
			p.Thumb = x.GetJPEGThumbnail()
		}
	case m.ImageMessage != nil:
		im := m.GetImageMessage()
		p.Kind, p.Text, p.Thumb, p.Media = KImage, im.GetCaption(), im.GetJPEGThumbnail(), true
		p.Extra.W, p.Extra.H, p.Extra.ViewOnce = im.GetWidth(), im.GetHeight(), im.GetViewOnce()
	case m.VideoMessage != nil || m.PtvMessage != nil:
		v := m.GetVideoMessage()
		if v == nil {
			v = m.GetPtvMessage()
		}
		p.Kind, p.Text, p.Thumb, p.Media = KVideo, v.GetCaption(), v.GetJPEGThumbnail(), true
		p.Extra.Seconds, p.Extra.GIF = v.GetSeconds(), v.GetGifPlayback()
		p.Extra.W, p.Extra.H, p.Extra.ViewOnce = v.GetWidth(), v.GetHeight(), v.GetViewOnce()
	case m.AudioMessage != nil:
		a := m.GetAudioMessage()
		p.Kind, p.Media = KAudio, true
		p.Extra.Seconds = a.GetSeconds()
		if a.GetPTT() {
			p.Text = "Messaggio vocale"
		} else {
			p.Text = "Audio"
		}
	case m.DocumentMessage != nil:
		d := m.GetDocumentMessage()
		p.Kind, p.Text, p.Thumb, p.Media = KDoc, d.GetCaption(), d.GetJPEGThumbnail(), true
		p.FileName = pick(d.GetFileName(), d.GetTitle())
		p.Extra.Size, p.Extra.Pages = d.GetFileLength(), d.GetPageCount()
	case m.StickerMessage != nil:
		s := m.GetStickerMessage()
		p.Kind, p.Media = KSticker, true
		p.Thumb = s.GetPngThumbnail()
		p.Extra.Animated = s.GetIsAnimated()
	case m.LocationMessage != nil:
		l := m.GetLocationMessage()
		p.Kind, p.Thumb = KLocation, l.GetJPEGThumbnail()
		p.Extra.Title, p.Extra.Desc = l.GetName(), l.GetAddress()
		p.Extra.URL = fmt.Sprintf("https://www.google.com/maps?q=%f,%f", l.GetDegreesLatitude(), l.GetDegreesLongitude())
		p.Text = l.GetComment()
	case m.LiveLocationMessage != nil:
		l := m.GetLiveLocationMessage()
		p.Kind, p.Thumb = KLocation, l.GetJPEGThumbnail()
		p.Extra.Title = "Posizione in tempo reale"
		p.Extra.URL = fmt.Sprintf("https://www.google.com/maps?q=%f,%f", l.GetDegreesLatitude(), l.GetDegreesLongitude())
		p.Text = l.GetCaption()
	case m.ContactMessage != nil:
		c := m.GetContactMessage()
		p.Kind = KContact
		p.Extra.Title = c.GetDisplayName()
		p.Extra.Options = phonesOf(c.GetVcard())
	case m.ContactsArrayMessage != nil:
		ca := m.GetContactsArrayMessage()
		p.Kind = KContact
		var names []string
		for _, c := range ca.GetContacts() {
			names = append(names, c.GetDisplayName())
		}
		p.Extra.Title = fmt.Sprintf("%d contatti", len(names))
		p.Extra.Options = names
	case m.PollCreationMessage != nil || m.PollCreationMessageV2 != nil || m.PollCreationMessageV3 != nil:
		pc := m.GetPollCreationMessage()
		if pc == nil {
			pc = m.GetPollCreationMessageV2()
		}
		if pc == nil {
			pc = m.GetPollCreationMessageV3()
		}
		p.Kind = KPoll
		p.Extra.Title = pc.GetName()
		for _, o := range pc.GetOptions() {
			p.Extra.Options = append(p.Extra.Options, o.GetOptionName())
		}
	case m.EventMessage != nil:
		ev := m.GetEventMessage()
		p.Kind = KEvent
		p.Extra.Title = ev.GetName()
		p.Extra.Desc = ev.GetDescription()
		if ev.GetStartTime() > 0 {
			p.Text = "Inizio: " + formatUnix(ev.GetStartTime())
		}
	case m.GroupInviteMessage != nil:
		g := m.GetGroupInviteMessage()
		p.Kind, p.Thumb = KText, g.GetJPEGThumbnail()
		p.Text = "Invito al gruppo «" + g.GetGroupName() + "»"
		if c := g.GetCaption(); c != "" {
			p.Text += "\n" + c
		}
	case m.ButtonsMessage != nil:
		p.Kind, p.Text = KText, m.GetButtonsMessage().GetContentText()
	case m.TemplateMessage != nil:
		t := m.GetTemplateMessage().GetHydratedTemplate()
		p.Kind, p.Text = KText, strings.TrimSpace(t.GetHydratedTitleText()+"\n"+t.GetHydratedContentText())
	case m.ListMessage != nil:
		l := m.GetListMessage()
		p.Kind, p.Text = KText, strings.TrimSpace(l.GetTitle()+"\n"+l.GetDescription())
	case m.InteractiveMessage != nil:
		im := m.GetInteractiveMessage()
		p.Kind, p.Text = KText, strings.TrimSpace(im.GetHeader().GetTitle()+"\n"+im.GetBody().GetText())
	case m.ButtonsResponseMessage != nil:
		p.Kind, p.Text = KText, m.GetButtonsResponseMessage().GetSelectedDisplayText()
	case m.ListResponseMessage != nil:
		p.Kind, p.Text = KText, m.GetListResponseMessage().GetTitle()
	case m.TemplateButtonReplyMessage != nil:
		p.Kind, p.Text = KText, m.GetTemplateButtonReplyMessage().GetSelectedDisplayText()
	case m.OrderMessage != nil:
		p.Kind, p.Text = KText, "🛒 Ordine: "+m.GetOrderMessage().GetOrderTitle()
	case m.ProductMessage != nil:
		pr := m.GetProductMessage().GetProduct()
		p.Kind, p.Text = KText, "🛍 "+pr.GetTitle()+"\n"+pr.GetDescription()
	case m.RequestPaymentMessage != nil || m.SendPaymentMessage != nil:
		p.Kind, p.Text = KText, "💳 Pagamento (aprilo sul telefono)"
	}
	if p.Kind == KText && p.Extra.URL == "" {
		if u := firstURL(p.Text); u != "" {
			p.Extra.URL = u
		}
	}
	return
}

func dur(s uint32) string { return fmt.Sprintf("%d:%02d", s/60, s%60) }

func humanSize(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d kB", n/(1<<10))
	case n > 0:
		return fmt.Sprintf("%d B", n)
	}
	return ""
}

// previewOf: testo breve per la lista chat e le notifiche.
func previewOf(kind, text, fileName string) string {
	switch kind {
	case KImage:
		return "📷 " + pick(text, "Foto")
	case KVideo:
		return "🎥 " + pick(text, "Video")
	case KAudio:
		return "🎤 " + pick(text, "Audio")
	case KDoc:
		return "📄 " + pick(fileName, "Documento")
	case KSticker:
		return "💟 Sticker"
	case KLocation:
		return "📍 Posizione"
	case KContact:
		return "👤 Contatto"
	case KPoll:
		return "📊 Sondaggio"
	case KEvent:
		return "📅 Evento"
	case KCall:
		return "📞 " + text
	case KDeleted:
		return "Messaggio eliminato"
	}
	return text
}

// ---- emoticon alla MSN ----

var smileys = map[string]string{
	":)": "🙂", ":-)": "🙂", ":D": "😃", ":-D": "😃", "xD": "😆", "XD": "😆",
	";)": "😉", ";-)": "😉", ":P": "😛", ":p": "😛", ":-P": "😛", ":(": "🙁", ":-(": "🙁",
	":'(": "😢", ":O": "😮", ":o": "😮", ":*": "😘", "<3": "❤️", "</3": "💔",
	"(y)": "👍", "(Y)": "👍", "(n)": "👎", "(N)": "👎", ":|": "😐", "B)": "😎", "8)": "😎",
	">:(": "😠", ":@": "😠", "^^": "😊", "^_^": "😊", ":S": "😖", ":$": "😳", "O:)": "😇",
}

var reToken = regexp.MustCompile(`\S+`)

// convertSmileys trasforma :) ;) <3 e simili in emoji, solo se sono parole a sé.
func convertSmileys(s string) string {
	return reToken.ReplaceAllStringFunc(s, func(t string) string {
		if e, ok := smileys[t]; ok {
			return e
		}
		return t
	})
}

func formatUnix(s int64) string {
	t := time.Unix(s, 0).Local()
	return dayLabel(t) + " alle " + t.Format("15:04")
}

// previewMsg: come previewOf, ma riconosce GIF e sticker animati.
func previewMsg(m *Message) string {
	if m.Kind == KVideo && m.Extra.GIF {
		return "GIF"
	}
	return previewOf(m.Kind, m.Text, m.FileName)
}
