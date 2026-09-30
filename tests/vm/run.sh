#!/bin/bash
# Banc d'essai en machines virtuelles : deux vrais systèmes (ancien et
# nouveau PC) reliés par un réseau privé, migration réelle avec recherche
# automatique, puis vérifications, redémarrage du nouveau PC et annulation.
#
#   tests/vm/run.sh DOSSIER_DES_BINAIRES IMAGE_ANCIEN [IMAGE_NOUVEAU]
#
# Les images sont des images « cloud » (qcow2 avec cloud-init) : Ubuntu,
# Debian… Sans /dev/kvm, QEMU émule (très lent, pour le développement).
# Variables : BANC_KERNEL et BANC_INITRD pour démarrer un noyau donné (image
# sans chargeur de démarrage), BANC_DIR pour garder les fichiers de travail,
# BANC_MEM (Mo, 3072 par défaut), BANC_GARDER=1 pour laisser les machines
# allumées à la fin (une relance reprend alors sans tout refaire).
set -euo pipefail
BIN=$(realpath "$1"); IMG_SRC=$(realpath "$2"); IMG_DST=$(realpath "${3:-$2}")
HERE=$(dirname "$(realpath "$0")")
W=${BANC_DIR:-$(mktemp -d /tmp/bernard-banc.XXXX)}
mkdir -p "$W"
MEM=${BANC_MEM:-3072}
declare -A PORT=([src]=10022 [dst]=10023) IP=([src]=10.77.0.1 [dst]=10.77.0.2) NAME=([src]=ancien-pc [dst]=nouveau-pc) NUM=([src]=1 [dst]=2)
log() { echo "== $*"; }

[ -f "$W/key" ] || ssh-keygen -q -N "" -t ed25519 -f "$W/key"
SSHOPT=(-i "$W/key" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=10 -o ServerAliveInterval=15)
on()  { local vm=$1; shift; ssh "${SSHOPT[@]}" -p "${PORT[$vm]}" banc@127.0.0.1 "$@"; }
put() { local vm=$1 dst=$2; shift 2; scp -q "${SSHOPT[@]}" -P "${PORT[$vm]}" "$@" "banc@127.0.0.1:$dst"; }

# Disque de données : formaté ici, présent dans les deux machines avec le
# même identifiant, comme un disque qu'on sort de l'ancien PC pour le
# mettre dans le nouveau. Monté seulement sur l'ancien.
if [ ! -f "$W/donnees.raw" ]; then
  mkdir -p "$W/donnees" && echo "rapport sur le disque de données" > "$W/donnees/rapport.txt"
  truncate -s 1G "$W/donnees.raw" && mkfs.ext4 -q -L DONNEES -d "$W/donnees" "$W/donnees.raw"
fi

seed() {
  local vm=$1 extra=""
  if [ $vm = dst ]; then
    # Compte provisoire créé à l'installation, à supprimer après migration.
    extra="  - name: tmp
    groups: [sudo]
    shell: /bin/bash
    lock_passwd: false
    plain_text_passwd: provisoire"
  fi
  cat > "$W/$vm-user-data" <<UD
#cloud-config
hostname: ${NAME[$vm]}
users:
  - name: banc
    groups: [sudo]
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys: ["$(cat "$W/key.pub")"]
$extra
ssh_pwauth: false
apt: {preserve_sources_list: true}
UD
  cat > "$W/$vm-network" <<ND
version: 2
ethernets:
  nat0:
    match: {macaddress: "52:54:00:12:00:0${NUM[$vm]}"}
    set-name: nat0
    dhcp4: true
  lan0:
    match: {macaddress: "52:54:00:77:00:0${NUM[$vm]}"}
    set-name: lan0
    addresses: [${IP[$vm]}/24]
ND
  cloud-localds -N "$W/$vm-network" "$W/$vm-seed.iso" "$W/$vm-user-data"
}

running() { [ -f "$W/$1.pid" ] && kill -0 "$(cat "$W/$1.pid")" 2>/dev/null; }

boot() {
  local vm=$1 img=$2 lan
  running "$vm" && return 0 # déjà démarrée (BANC_GARDER)
  [ $vm = src ] && lan="listen=127.0.0.1:10777" || lan="connect=127.0.0.1:10777"
  [ -f "$W/$vm.qcow2" ] || qemu-img create -q -f qcow2 -b "$img" -F qcow2 "$W/$vm.qcow2" 20G
  [ -f "$W/$vm-donnees.raw" ] || cp --sparse=always "$W/donnees.raw" "$W/$vm-donnees.raw"
  local accel=tcg cpu=max kern=()
  [ -w /dev/kvm ] && accel=kvm cpu=host
  if [ -n "${BANC_KERNEL:-}" ]; then
    kern=(-kernel "$BANC_KERNEL" -initrd "$BANC_INITRD" -append "root=LABEL=cloudimg-rootfs ro console=ttyS0")
  fi
  qemu-system-x86_64 -name "$vm" -machine "q35,accel=$accel" -cpu "$cpu" -m "$MEM" -smp 2 \
    -drive "file=$W/$vm.qcow2,if=virtio" \
    -drive "file=$W/$vm-seed.iso,if=virtio,format=raw,readonly=on" \
    -drive "file=$W/$vm-donnees.raw,if=virtio,format=raw" \
    -netdev "user,id=nat,hostfwd=tcp:127.0.0.1:${PORT[$vm]}-:22" -device "virtio-net-pci,netdev=nat,mac=52:54:00:12:00:0${NUM[$vm]}" \
    -netdev "socket,id=lan,$lan" -device "virtio-net-pci,netdev=lan,mac=52:54:00:77:00:0${NUM[$vm]}" \
    -display none -serial "file:$W/$vm-console.log" -daemonize -pidfile "$W/$vm.pid" "${kern[@]}"
}

wait_ssh() {
  local vm=$1 limit=${2:-900}
  for ((t = 0; t < limit; t += 5)); do
    on "$vm" "cloud-init status --wait >/dev/null 2>&1; true" 2>/dev/null && return 0
    sleep 5
  done
  echo "La machine $vm ne répond pas (journal : $W/$vm-console.log)"; tail -30 "$W/$vm-console.log"; return 1
}

stop_all() { [ -n "${BANC_GARDER:-}" ] && return 0; for vm in src dst; do [ -f "$W/$vm.pid" ] && kill "$(cat "$W/$vm.pid")" 2>/dev/null || true; done; }
trap stop_all EXIT
trap 'echo "Arrêt : commande en échec à la ligne $LINENO de run.sh"' ERR

log "Démarrage des deux machines ($([ -w /dev/kvm ] && echo KVM || echo émulation lente))"
seed src; seed dst
boot src "$IMG_SRC"; sleep 1; boot dst "$IMG_DST"
wait_ssh src; wait_ssh dst
on src "cat /etc/os-release | grep PRETTY"; on dst "cat /etc/os-release | grep PRETTY"

log "Préparation de l'ancien PC"
put src /tmp/ "$BIN/bernard-agent" "$HERE/source-setup.sh"
if ! on src "sudo test -f /root/banc/empreintes.txt"; then
  on src "sudo bash /tmp/source-setup.sh" > "$W/source-setup.log" 2>&1 || { tail -40 "$W/source-setup.log"; exit 1; }
fi
on src "sudo install -m 755 /tmp/bernard-agent /usr/local/bin/"
put dst /tmp/ "$BIN/bernard" "$HERE/verify.sh"
on dst "sudo install -m 755 /tmp/bernard /usr/local/bin/ && sudo mkdir -p /root/banc && sudo install -m 755 /tmp/verify.sh /root/banc/"
on src "sudo cat /root/banc/empreintes.txt /root/banc/uuid-donnees; if sudo test -f /root/banc/depot-tiers; then echo DEPOT; fi" > "$W/src-infos"
# Le nouveau PC a déjà MariaDB, avec sa propre base : Bernard doit la
# mettre de côté en entier, et l'annulation la rendre intacte.
if ! on dst "sudo test -f /root/banc/base-cible"; then
  on dst "sudo DEBIAN_FRONTEND=noninteractive apt-get update -q && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q --no-install-recommends mariadb-server && sudo mysql -e 'CREATE DATABASE cible' && sudo touch /root/banc/base-cible" > "$W/target-setup.log" 2>&1 || { tail -20 "$W/target-setup.log"; exit 1; }
fi
on src "sudo cat /root/banc/empreintes.txt" | on dst "sudo tee /root/banc/empreintes.txt >/dev/null"
on src "sudo cat /root/banc/uuid-donnees" | on dst "sudo tee /root/banc/uuid-donnees >/dev/null"
if grep -q DEPOT "$W/src-infos"; then on dst "sudo touch /root/banc/depot-tiers"; fi
if [ "$(on src '. /etc/os-release; echo $ID $VERSION_ID')" = "$(on dst '. /etc/os-release; echo $ID $VERSION_ID')" ]; then
  on dst "sudo touch /root/banc/meme-version"
fi

log "Migration (recherche automatique sur le réseau privé)"
on dst "sudo sh -c 'nohup bernard receive --system --yes </dev/null >/root/banc/receive.log 2>&1 &'"
CODE=""
for _ in $(seq 1 60); do
  CODE=$(on dst "sudo grep -Eo '(Code d.appairage|Pairing code) *: *[0-9 ]+' /root/banc/receive.log" 2>/dev/null | sed 's/.*: *//; s/ //g' || true)
  [ -n "$CODE" ] && break; sleep 2
done
[ -n "$CODE" ] || { echo "Pas de code affiché"; on dst "sudo cat /root/banc/receive.log"; exit 1; }
on src "sudo bernard-agent connect --code $CODE --timeout 20m" > "$W/agent.log" 2>&1 || { echo "Échec côté ancien PC :"; tail -30 "$W/agent.log"; }
for _ in $(seq 1 180); do on dst "pgrep -x bernard >/dev/null" || break; sleep 5; done
on dst "sudo cat /root/banc/receive.log" > "$W/receive.log" || true

FAILS=0
log "Vérifications"
on dst "sudo bash /root/banc/verify.sh apres" || FAILS=$((FAILS + $?))
if on src "systemctl is-active --quiet mariadb && systemctl is-active --quiet docker"; then
  echo "  ok    ancien PC : MariaDB et Docker relancés après la copie"
else echo "  ÉCHEC ancien PC : MariaDB et Docker relancés après la copie"; FAILS=$((FAILS + 1)); fi

log "Redémarrage du nouveau PC (disque rattaché, compte provisoire supprimé)"
on dst "sudo bernard remove-account --at-boot tmp"
on dst "sudo systemctl reboot" || true
sleep 20; wait_ssh dst
on dst "sudo bash /root/banc/verify.sh redemarrage" || FAILS=$((FAILS + $?))

log "Annulation"
on dst "sudo sh -c 'bernard undo --journal \$(ls /var/lib/bernard/*/journal.jsonl | head -1)'" > "$W/undo.log" 2>&1 || true
on dst "sudo bash /root/banc/verify.sh annulation" || FAILS=$((FAILS + $?))

echo
if [ $FAILS -eq 0 ]; then echo "Banc d'essai réussi."; exit 0; fi
echo "$FAILS vérification(s) en échec. Fichiers de travail : $W"; exit 1
