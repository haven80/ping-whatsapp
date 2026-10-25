#!/bin/sh
# Installa Ping per l'utente corrente (niente sudo: va bene anche su Bazzite e altri sistemi immutabili).
#   ./install.sh            installa o aggiorna
#   ./install.sh --remove   disinstalla (i dati e il collegamento a WhatsApp restano in ~/.config/Ping)
set -e
cd "$(dirname "$0")"
BIN="$HOME/.local/bin"
APPS="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICONS="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"

if [ "$1" = "--remove" ]; then
    rm -f "$BIN/ping-whatsapp" "$APPS/ping-whatsapp.desktop" "$ICONS/ping-whatsapp.png"
    echo "Ping disinstallato. I dati sono ancora in ~/.config/Ping (cancellali a mano se vuoi)."
    exit 0
fi

mkdir -p "$BIN" "$APPS" "$ICONS"
install -m 755 ping-whatsapp "$BIN/ping-whatsapp"
install -m 644 ping-whatsapp.png "$ICONS/ping-whatsapp.png"
# percorso completo: alcuni menu non vedono ~/.local/bin nel PATH
sed "s|^Exec=.*|Exec=$BIN/ping-whatsapp|" ping-whatsapp.desktop > "$APPS/ping-whatsapp.desktop"
chmod 644 "$APPS/ping-whatsapp.desktop"
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$APPS" >/dev/null 2>&1 || true
command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q "${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor" >/dev/null 2>&1 || true

echo "Ping installato: lo trovi nel menu delle applicazioni."
for t in ffmpeg wl-paste; do
    command -v $t >/dev/null 2>&1 || MISSING="$MISSING $t"
done
if [ -n "$MISSING" ]; then
    echo "Facoltativi, non trovati:$MISSING"
    echo "  ffmpeg   -> GIF animate (invio e riproduzione)"
    echo "  wl-paste -> incollare immagini con Ctrl+V (pacchetto wl-clipboard; su X11 serve xclip)"
fi
