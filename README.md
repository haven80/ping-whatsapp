# Ping — client WhatsApp leggero per Windows

Lista contatti in stile MSN/ICQ, una finestra separata per ogni chat. Niente browser incorporato: interfaccia nativa disegnata con Gio, collegamento a WhatsApp tramite la libreria open source whatsmeow.

## Uso

1. Avvia `Ping.exe`.
2. Sul telefono: WhatsApp › Impostazioni › Dispositivi collegati › Collega un dispositivo, e inquadra il QR.
3. Dopo la sincronizzazione compare la lista delle chat. Un clic apre la conversazione in una finestra a sé.

- **Invio**: invia. **Maiusc+Invio**: va a capo.
- Passa col mouse su un messaggio: **↩ Rispondi**, **🙂 Reagisci** (6 reazioni rapide, oppure "+" per sceglierne una qualsiasi), **✎ Modifica** (tuoi messaggi di testo, entro 15 minuti) e **🗑 Elimina** ("per tutti" sui tuoi messaggi entro circa due giorni e mezzo, oppure "per me").
- **Tasto destro** su un messaggio: menu con Rispondi, Reagisci, Inoltra, Copia, Modifica, Elimina.
- **↪ Inoltra**: scegli fino a 5 chat (con ricerca, anche nella rubrica). Testo, foto, video, GIF, vocali, documenti e sticker arrivano con l'etichetta "Inoltrato". Clic su una citazione: salta al messaggio originale. Esc annulla.
- Faccina: pannello emoji (con le recenti), più le schede **Sticker** e **GIF**: cerca online, reinvia quelli già ricevuti, oppure creane uno da un'immagine o una GIF del PC.
- Faccina: pannello emoji (con le recenti). Si possono scrivere anche le classiche :) ;) :D <3 (y), che diventano emoji all'invio.
- Graffetta: invia foto (jpg/png) o documenti.
- **Trascina** file da Esplora risorse dentro la finestra della chat: diventano allegati, come con Ctrl+V.
- Nei gruppi, scrivi **@** per menzionare qualcuno: compare l'elenco dei partecipanti (frecce + Invio/Tab, oppure clic). Le menzioni ricevute mostrano il nome invece del numero.
- Nei gruppi, **clic sul nome** (o sulla foto) di un partecipante: apre la chat privata con lui.
- **Ctrl+V** nel campo di testo: incolla immagini (screenshot, foto copiate dal browser) o file copiati in Esplora risorse. Compaiono sopra il campo; Invio li manda, e il testo scritto diventa la didascalia.
- Clic su foto, video, vocali o documenti ricevuti: li scarica e li apre con il programma predefinito.
- Chiudere la finestra principale la nasconde nell'area di notifica. Per uscire: clic destro sull'icona › Esci.
- Ingranaggio in alto: **Opzioni** (tema Sistema/Chiaro/Scuro, foto nella lista, foto accanto ai messaggi, suoni, notifiche, chiave KLIPY, disconnessione).

### Ricerca GIF e sticker online
Tenor (Google) ha chiuso la sua API il 30 giugno 2026; Ping usa KLIPY, lo stesso servizio adottato da WhatsApp. È gratuito ma richiede una chiave personale: creala su https://partner.klipy.com e incollala in Opzioni › GIF e sticker online.

## Cosa c'è

Foto e sticker a piena qualità dentro la chat, video con anteprima e durata, vocali, documenti (tipo, pagine, dimensione), posizioni con mappa, contatti, sondaggi, eventi, anteprime dei link, risposte citate, reazioni, messaggi inoltrati e "visualizza una volta", chiamate in arrivo (da rispondere sul telefono), emoji a colori. Chat recenti e rubrica, ricerca, foto profilo, stato online e "sta scrivendo…", conteggio non letti, spunte di consegna e lettura, gruppi con nome del mittente, archiviate, messaggi modificati ed eliminati, avvisi di Windows con suono e lampeggio sulla barra, tema chiaro/scuro automatico.

## Cosa non c'è

Chiamate vocali e video (si vedono ma si rispondono dal telefono), voti nei sondaggi, registrazione di vocali, stati. I video normali si aprono nel lettore di Windows (le GIF invece si animano nella chat).

## Dati

Tutto in `%APPDATA%\Ping`: sessione (`session.db`), chat e messaggi (`ping.db`), foto profilo e file scaricati. Cancellando la cartella si riparte da zero.

Se qualcosa va storto, l'errore viene scritto in `crash.log` nella stessa cartella: il programma prova a proseguire invece di chiudersi.

`settings.json` accetta anche `"theme": "light"` o `"dark"` per forzare il tema.

## Avvertenze

Usare un client non ufficiale va contro i termini di servizio di WhatsApp. Per un uso personale normale i blocchi sono rari, ma il rischio non è zero.

## Compilare dai sorgenti

### Con GitHub (automatico)

Il file `.github/workflows/build.yml` compila Ping a ogni push, su server Linux di GitHub:

- l'exe si scarica da **Actions** › l'esecuzione più recente › **Artifacts**;
- creando un tag che inizia con `v` (es. `git tag v1.0.0 && git push --tags`) viene pubblicata una **Release** con `Ping.exe` (x64) e `Ping-arm64.exe` (PC Windows ARM) già allegati.

### A mano

Serve Go 1.26 o successivo. Niente cgo, si compila anche da Linux:

```
go run github.com/tc-hib/go-winres@latest make --arch amd64
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o Ping.exe .
```

`Ping.exe -demo` mostra l'interfaccia con conversazioni finte, senza collegarsi.

## Licenze di terze parti

- [whatsmeow](https://github.com/tulir/whatsmeow) — Mozilla Public License 2.0
- [Gio](https://gioui.org) — MIT / Unlicense
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) — BSD
- [nativewebp](https://github.com/HugoSmits86/nativewebp), [go-qrcode](https://github.com/skip2/go-qrcode) — MIT
- Font Noto Color Emoji — SIL Open Font License (vedi `fonts/`)
