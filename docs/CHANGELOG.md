# Historique des versions

## 0.1.1 — 26 septembre 2026

- `bernard-agent connect` attend le nouvel ordinateur **sans limite de durée**
  au lieu d'abandonner après 4 secondes. On peut lancer l'agent sur l'ancien
  ordinateur, puis installer Linux sur le nouveau et y ouvrir Bernard des
  heures plus tard. L'écoute est passive : aucun trafic réseau. Un message
  « toujours en attente » s'affiche chaque minute ; `--timeout 2h` fixe une
  limite, Ctrl+C abandonne.
- L'inventaire de l'ancien ordinateur est fait **au moment où le nouveau est
  trouvé**, et non plus au lancement : les fichiers créés ou modifiés pendant
  l'attente sont bien envoyés.
- La mise en veille de l'ancien ordinateur est **bloquée** pendant l'attente
  et le transfert (`systemd-inhibit`, relâché automatiquement à la fin de
  l'agent, même en cas d'arrêt brutal). En administrateur, la fermeture du
  capot d'un portable ne le met pas non plus en veille. Même protection pour
  `bernard-agent pack`.

## 0.1.0 — 26 septembre 2026

Première version publique : migration Linux → Linux (jalon V1.0).
