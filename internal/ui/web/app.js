"use strict";

// Le jeton arrive dans l'URL une seule fois ; on le retire aussitôt.
const params = new URLSearchParams(location.search);
const TOKEN = params.get("t") || sessionStorage.getItem("bernard-token") || "";
sessionStorage.setItem("bernard-token", TOKEN);
history.replaceState(null, "", "/");

const main = document.getElementById("main");
let state = null;
let shown = "";          // écran actuellement affiché
const ui = {             // état local de l'écran de choix
  selected: {}, passwords: {}, showSkipped: false,
};

// ------------------------------------------------------------ utilitaires

const nf = new Intl.NumberFormat("fr-BE", { maximumFractionDigits: 1 });
function bytes(n) {
  const u = ["o", "ko", "Mo", "Go", "To"];
  let i = 0;
  while (n >= 1000 && i < u.length - 1) { n /= 1000; i++; }
  return nf.format(n) + " " + u[i];
}
function dateFR(iso) {
  return new Date(iso).toLocaleDateString("fr-BE", { day: "numeric", month: "long", year: "numeric" });
}
function since(iso) {
  const months = Math.round((Date.now() - new Date(iso)) / (30.44 * 864e5));
  if (months < 1) return "ce mois-ci";
  if (months < 12) return `il y a ${months} mois`;
  const y = Math.floor(months / 12), m = months % 12;
  return `il y a ${y} an${y > 1 ? "s" : ""}` + (m ? ` et ${m} mois` : "");
}
function plural(n, one, many) { return `${nf.format(n)} ${n > 1 ? many : one}`; }
const DISTROS = { zorin: "Zorin OS", ubuntu: "Ubuntu", linuxmint: "Linux Mint", debian: "Debian", pop: "Pop!_OS", elementary: "elementary OS" };
function distro(src) { return src ? `${DISTROS[src.distro] || src.distro || src.os} ${src.version || ""}`.trim() : ""; }
function duration(sec) {
  if (sec < 90) return "moins de 2 min";
  if (sec < 5400) return `${Math.round(sec / 60)} min`;
  const h = Math.floor(sec / 3600), m = Math.round((sec % 3600) / 60);
  return `${h} h ${String(m).padStart(2, "0")}`;
}
function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
// Typographie française : espace insécable avant ; : ! ? » et après «.
const NBSP = "\u00a0";
const fr = str => String(str).replace(/ ([;:!?»])/g, NBSP + "$1").replace(/« /g, "«" + NBSP);
function frTypo(root) {
  const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let n = w.nextNode(); n; n = w.nextNode()) {
    if (n.parentElement?.closest(".cmd, pre, code")) continue;
    n.nodeValue = fr(n.nodeValue);
  }
  return root;
}
function el(html) {
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return frTypo(t.content);
}

async function api(path, body) {
  const r = await fetch("/api/" + path, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Bernard-Token": TOKEN },
    body: JSON.stringify(body || {}),
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || "Erreur inattendue");
  return data;
}

function confirmBox(title, text, okLabel, danger) {
  const d = document.getElementById("confirm");
  document.getElementById("confirm-title").textContent = title;
  document.getElementById("confirm-text").textContent = text;
  const ok = document.getElementById("confirm-ok");
  ok.textContent = okLabel;
  ok.className = danger ? "danger" : "primary";
  d.showModal();
  return new Promise(res => d.addEventListener("close", () => res(d.returnValue === "ok"), { once: true }));
}

const RAILS = {
  target: { steps: ["Source", "Connexion", "Analyse", "Choix", "Migration", "Bilan"],
    note: "Rien n'est jamais modifié sur l'ancien ordinateur." },
  source: { steps: ["Rôle", "Destination", "Connexion", "Envoi", "Terminé"],
    note: "Rien n'est modifié sur cet ordinateur : ses données sont seulement lues." },
  none: { steps: ["Bienvenue"], note: "Lancez Bernard sur les deux ordinateurs, dans l'ordre que vous voulez." },
};
let railKind = "";
function setRail(n) {
  const kind = state?.role || "none";
  if (kind !== railKind) {
    railKind = kind;
    const r = RAILS[kind] || RAILS.none;
    document.getElementById("steps").replaceChildren(...r.steps.map((label, i) => {
      const li = document.createElement("li");
      li.dataset.step = i + 1; li.textContent = label; return li;
    }));
    document.querySelector(".rail-note").textContent = r.note;
  }
  document.querySelectorAll("#steps li").forEach(li => {
    const i = +li.dataset.step;
    li.classList.toggle("done", i < n);
    li.classList.toggle("current", i === n);
    if (i === n) li.setAttribute("aria-current", "step"); else li.removeAttribute("aria-current");
  });
}

function showError(msg, where) {
  const box = (where || main).querySelector(".form-error");
  if (box) { box.textContent = msg; box.hidden = !msg; }
}

// ------------------------------------------------------------ écrans

const ICONS = {
  target: `<svg viewBox="0 0 48 48" aria-hidden="true"><rect x="6" y="8" width="36" height="24" rx="3"/><path d="M18 40h12M24 32v8"/><path d="M24 14v12M18 20l6 6 6-6"/></svg>`,
  source: `<svg viewBox="0 0 48 48" aria-hidden="true"><rect x="8" y="12" width="32" height="20" rx="3"/><path d="M4 38h40"/><path d="M24 28V16M18 22l6-6 6 6"/></svg>`,
  network: `<svg viewBox="0 0 48 48" aria-hidden="true"><rect x="4" y="10" width="16" height="12" rx="2"/><rect x="28" y="26" width="16" height="12" rx="2"/><path d="M12 22v10h16"/></svg>`,
  disk: `<svg viewBox="0 0 48 48" aria-hidden="true"><rect x="8" y="12" width="32" height="24" rx="4"/><circle cx="33" cy="30" r="2"/><path d="M14 20h14"/></svg>`,
};
const PHASES = {
  prepare: ["Préparation", "Bernard fait l'inventaire de cet ordinateur. Rien n'est modifié."],
  analysing: ["Le nouvel ordinateur analyse vos données", "Il prépare la liste de ce qui peut être transféré."],
  choose: ["Faites votre choix sur le nouvel ordinateur", "Cochez sur le nouvel ordinateur ce qui vient avec vous, puis lancez la migration. Cet ordinateur attend."],
  system: ["Installation des comptes et des applications", "Le nouvel ordinateur installe vos applications. Cela peut prendre plusieurs minutes ; l'envoi des fichiers suit."],
  copy: ["Envoi en cours", "Vous pouvez continuer à utiliser cet ordinateur, mais évitez de modifier des fichiers."],
  settings: ["Derniers réglages", "Le nouvel ordinateur applique vos réglages. C'est presque fini."],
};

function speedOf(bytesNow) {
  const now = Date.now();
  speedSamples.push([now, bytesNow]);
  speedSamples = speedSamples.filter(([t]) => now - t < 10000);
  const [t0, b0] = speedSamples[0];
  const span = (now - t0) / 1000;
  return span >= 2 ? (bytesNow - b0) / span : 0;
}

function codeBoxes() {
  return `<div class="code-entry" role="group" aria-label="Code à 6 chiffres">
    ${[0, 1, 2].map(i => `<input inputmode="numeric" pattern="[0-9]" maxlength="1" autocomplete="off" aria-label="Chiffre ${i + 1}">`).join("")}
    <span class="gap"></span>
    ${[3, 4, 5].map(i => `<input inputmode="numeric" pattern="[0-9]" maxlength="1" autocomplete="off" aria-label="Chiffre ${i + 1}">`).join("")}
  </div>`;
}

function wireCode(onFull) {
  const boxes = [...main.querySelectorAll(".code-entry input")];
  const value = () => boxes.map(b => b.value).join("");
  boxes.forEach((b, i) => {
    b.addEventListener("input", () => {
      b.value = b.value.replace(/\D/g, "").slice(-1);
      if (b.value && i < 5) boxes[i + 1].focus();
      if (value().length === 6) onFull(value());
    });
    b.addEventListener("keydown", e => {
      if (e.key === "Backspace" && !b.value && i > 0) { boxes[i - 1].focus(); boxes[i - 1].value = ""; }
      if (e.key === "ArrowLeft" && i > 0) boxes[i - 1].focus();
      if (e.key === "ArrowRight" && i < 5) boxes[i + 1].focus();
    });
    b.addEventListener("paste", e => {
      const d = (e.clipboardData.getData("text") || "").replace(/\D/g, "").slice(0, 6);
      if (!d) return;
      e.preventDefault();
      d.split("").forEach((c, k) => { if (boxes[k]) boxes[k].value = c; });
      boxes[Math.min(d.length, 5)].focus();
      if (value().length === 6) onFull(value());
    });
  });
  boxes[0].focus();
  return { clear() { boxes.forEach(b => b.value = ""); boxes[0].focus(); }, set(disabled) { boxes.forEach(b => b.disabled = disabled); } };
}

const screens = {
  role() {
    setRail(1);
    main.replaceChildren(el(`
      <h1>Bienvenue dans Bernard</h1>
      <p class="lead">Bernard déménage vos comptes, vos documents, vos réglages et vos applications
      d'un ordinateur à l'autre. Ouvrez-le sur les deux ordinateurs, dans l'ordre que vous voulez.</p>
      <div class="choices">
        <button class="choice" data-role="target">${ICONS.target}
          <div><strong>Ceci est le nouvel ordinateur</strong>
          <span>Recevoir les données d'un ancien ordinateur ou d'un disque externe.</span></div>
        </button>
        <button class="choice" data-role="source">${ICONS.source}
          <div><strong>Ceci est l'ancien ordinateur</strong>
          <span>Envoyer ses données vers le nouvel ordinateur, ou les préparer sur un disque externe.
          Rien n'y est modifié.</span></div>
        </button>
      </div>
      <p class="form-error banner error" hidden></p>`));
    main.querySelectorAll(".choice").forEach(b => b.addEventListener("click", async () => {
      try { await api("role", { role: b.dataset.role }); } catch (e) { showError(e.message); }
    }));
  },

  "src-home"() {
    setRail(2);
    main.replaceChildren(el(`
      <h1>Transférer depuis cet ordinateur</h1>
      <p class="lead">Comment voulez-vous transmettre vos données au nouvel ordinateur ?</p>
      <div class="choices">
        <button class="choice" data-mode="network">${ICONS.network}
          <div><strong>Directement au nouvel ordinateur</strong>
          <span>Par le Wi-Fi ou un câble réseau. Les deux ordinateurs doivent être allumés ; le nouveau peut être
          encore en cours d'installation, cet ordinateur l'attendra.</span></div>
        </button>
        <button class="choice" data-mode="disk">${ICONS.disk}
          <div><strong>Sur un disque externe</strong>
          <span>Un paquet chiffré, à brancher ensuite sur le nouvel ordinateur. Utile sans réseau commun.</span></div>
        </button>
      </div>
      <p class="form-error banner error" hidden></p>
      <div class="bar"><span class="summary"></span><button data-act="reset" class="quiet">Revenir</button></div>`));
    main.querySelectorAll(".choice").forEach(b => b.addEventListener("click", async () => {
      try { await api("source", { mode: b.dataset.mode }); } catch (e) { showError(e.message); }
    }));
    main.querySelector("[data-act=reset]").addEventListener("click", () => api("reset"));
  },

  "src-wait"(s) {
    setRail(2);
    main.replaceChildren(el(`
      <h1>En attente du nouvel ordinateur</h1>
      <p class="lead">Sur le nouvel ordinateur, ouvrez Bernard, choisissez « Ceci est le nouvel ordinateur »
      puis « Depuis un autre ordinateur ». Il apparaîtra ici tout seul.</p>
      <div class="radar" aria-hidden="true">${ICONS.source}<span class="dots"><i></i><i></i><i></i></span>${ICONS.target}</div>
      <p class="waiting"><span class="pulse"></span> <span id="w-text">Recherche sur le Wi-Fi et les câbles réseau…</span></p>
      <p class="links-hint">Le nouvel ordinateur est encore en cours d'installation ? Aucun problème : cet ordinateur
      l'attend aussi longtemps qu'il le faut, sans charger le réseau, et ne se mettra pas en veille.</p>
      <p class="banner warn" id="w-hint" hidden>Toujours rien ? Vérifiez que les deux ordinateurs sont sur le même réseau.
      Certains Wi-Fi (invités, entreprise) empêchent les appareils de se voir : reliez-les alors par un câble réseau.</p>
      <p class="form-error banner error" hidden></p>
      <div class="bar"><span class="summary"></span><button data-act="reset" class="quiet">Annuler</button></div>`));
    main.querySelector("[data-act=reset]").addEventListener("click", () => api("reset"));
    updateWait(s);
  },

  "src-code"(s) {
    setRail(3);
    main.replaceChildren(el(`
      <h1>Nouvel ordinateur trouvé</h1>
      <div id="targets"></div>
      <p class="lead">Saisissez le code à 6 chiffres affiché sur le nouvel ordinateur.</p>
      ${codeBoxes()}
      <p class="waiting" id="c-busy" hidden><span class="pulse"></span> Connexion…</p>
      <p class="form-error banner error" hidden></p>
      <div class="bar"><span class="summary" id="c-inv"></span><button data-act="reset" class="quiet">Annuler</button></div>`));
    main.querySelector("[data-act=reset]").addEventListener("click", () => api("reset"));
    ui.code = wireCode(async code => {
      try { ui.code.set(true); await api("pair", { target: ui.target || "", code }); }
      catch (e) { showError(e.message); ui.code.set(false); ui.code.clear(); }
    });
    ui.targetsKey = "";
    updateCode(s);
  },

  "src-send"(s) {
    setRail(4);
    main.replaceChildren(el(`
      <h1 id="p-title"></h1>
      <p class="lead" id="p-lead"></p>
      <div id="link"></div>
      <div id="p-copy">
        <div class="tide" role="progressbar" aria-valuemin="0" aria-valuemax="100" id="bar"><i></i></div>
        <div class="figures">
          <div><span>Envoyé</span><b id="f-bytes"></b></div>
          <div><span>Débit</span><b id="f-speed"></b></div>
          <div><span>Reste environ</span><b id="f-eta"></b></div>
          <div><span>Liaison</span><b id="f-link"></b></div>
        </div>
        <p class="current-file" id="f-file"></p>
      </div>
      <p class="waiting" id="p-wait"><span class="pulse"></span> <span id="p-wait-text"></span></p>
      <p class="links-hint" id="p-cable" hidden>Pour aller plus vite, branchez un câble réseau entre les deux ordinateurs
      (ou vers la box) : Bernard basculera dessus tout seul, sans rien interrompre.</p>
      <details><summary>Journal détaillé</summary><pre class="log" id="log"></pre></details>
      <div class="bar"><span class="summary">Une coupure n'est pas grave : l'envoi reprend là où il s'était arrêté.</span>
        <button data-act="stop" class="danger">Arrêter</button></div>`));
    main.querySelector("[data-act=stop]").addEventListener("click", async () => {
      if (await confirmBox("Arrêter l'envoi ?", "Ce qui est déjà arrivé et vérifié sur le nouvel ordinateur est conservé.", "Arrêter", true)) api("stop");
    });
    updateSend(s);
  },

  "src-disk"(s) {
    setRail(2);
    const disks = s.disks || [];
    const inv = s.inventory;
    main.replaceChildren(el(`
      <h1>Préparer un disque externe</h1>
      <p class="lead">Bernard y écrit un paquet chiffré avec vos comptes, documents, réglages et la liste de vos applications.</p>
      ${disks.length ? `<div class="choices" id="disks">${disks.map((d, i) => `
        <label class="choice disk"><input type="radio" name="disk" value="${esc(d.path)}" ${i === 0 ? "checked" : ""}>
          <div><strong>${esc(d.label)}</strong><span>${bytes(d.free)} libres</span></div></label>`).join("")}</div>`
        : `<p class="banner warn">Aucun disque externe détecté. Branchez un disque ou une clé USB, puis
          <button class="link" data-act="refresh">cherchez à nouveau</button>.</p>`}
      <p class="meta" id="d-need">${inv ? `Il faut environ ${bytes(inv.bytes)} d'espace libre.` : "Inventaire de cet ordinateur en cours…"}</p>
      <label class="field">Phrase de passe (8 caractères au moins)
        <input id="p1" type="password" autocomplete="new-password"></label>
      <label class="field">Confirmation
        <input id="p2" type="password" autocomplete="new-password"></label>
      <p class="meta">Notez-la : elle sera demandée sur le nouvel ordinateur, et personne ne peut ouvrir le paquet sans elle.</p>
      <p class="form-error banner error" hidden></p>
      <div class="bar"><span class="summary"></span>
        <button data-act="reset" class="quiet">Revenir</button>
        <button data-act="go" class="primary" ${disks.length ? "" : "disabled"}>Écrire le paquet</button></div>`));
    main.querySelector("[data-act=reset]").addEventListener("click", () => api("reset"));
    main.querySelector("[data-act=refresh]")?.addEventListener("click", () => api("disks"));
    main.querySelector("[data-act=go]").addEventListener("click", async () => {
      const a = main.querySelector("#p1").value, b = main.querySelector("#p2").value;
      if (a.length < 8) return showError("La phrase de passe doit faire au moins 8 caractères.");
      if (a !== b) return showError("Les deux phrases de passe diffèrent.");
      const disk = main.querySelector("input[name=disk]:checked")?.value;
      try { await api("pack", { disk, passphrase: a }); } catch (e) { showError(e.message); }
    });
    ui.disksKey = JSON.stringify(disks.map(d => d.path));
  },

  "src-done"(s) {
    setRail(5);
    const p = s.send || {};
    const disk = !!p.dest;
    main.replaceChildren(el(`${disk ? `
      <h1>Le paquet est prêt</h1>
      <p class="lead">${plural(p.files || 0, "fichier écrit", "fichiers écrits")} (${bytes(p.bytes || 0)}), chiffrés.</p>
      <ol class="howto">
        <li><span>Éjectez le disque, puis branchez-le sur le nouvel ordinateur.</span></li>
        <li><span>Ouvrez Bernard sur le nouvel ordinateur : « Ceci est le nouvel ordinateur », puis « Depuis un disque externe ».</span></li>
        <li><span>Saisissez la phrase de passe choisie ici.</span></li>
      </ol>
      <p class="meta">Dossier : <span class="cmd">${esc(p.dest)}</span></p>
      ${(p.errors || []).length ? `<details><summary>${p.errors.length} éléments non inclus</summary><pre class="log">${esc(p.errors.join("\n"))}</pre></details>` : ""}` : `
      <h1>Transfert terminé</h1>
      <p class="lead">Vos données sont arrivées sur ${esc(s.peer || "le nouvel ordinateur")}, et chaque fichier y a été vérifié.
      Rien n'a été modifié sur cet ordinateur.</p>
      <ul class="results"><li>${plural(p.files || 0, "fichier envoyé", "fichiers envoyés")} (${bytes(p.bytes || 0)})</li></ul>
      <p>Gardez cet ordinateur tel quel tant que vous n'avez pas tout vérifié sur le nouveau.</p>`}
      <div class="bar"><span class="summary"></span><button data-act="quit" class="primary">Fermer Bernard</button></div>`));
    main.querySelector("[data-act=quit]").addEventListener("click", () => { api("quit"); window.close(); });
  },

  "src-stopped"(s) {
    setRail(4);
    main.replaceChildren(el(`
      <h1>Envoi interrompu</h1>
      <p class="lead">Ce qui est déjà arrivé et vérifié sur le nouvel ordinateur ne sera pas renvoyé.</p>
      ${s.error ? `<p class="banner error">${esc(s.error)}</p>` : ""}
      <p>Pour reprendre, cliquez sur « Reprendre » : Bernard retrouvera le nouvel ordinateur, qui affiche un
      nouveau code. Si Bernard y a été fermé entre-temps, rouvrez-le d'abord (« Depuis un autre ordinateur »).
      La migration continuera là où elle s'était arrêtée.</p>
      <div class="bar"><span class="summary"></span>
        <button data-act="quit" class="quiet">Fermer Bernard</button>
        <button data-act="again" class="primary">Reprendre</button></div>`));
    main.querySelector("[data-act=quit]").addEventListener("click", () => { api("quit"); window.close(); });
    main.querySelector("[data-act=again]").addEventListener("click", async () => {
      try { await api("source", { mode: "network" }); } catch (e) { alert(e.message); }
    });
  },

  welcome() {
    setRail(1);
    main.replaceChildren(el(`
      <h1>Transférer vers cet ordinateur</h1>
      <p class="lead">Bernard installe ici vos comptes, vos documents, vos réglages et vos applications,
      en les reprenant de votre ancien ordinateur.</p>
      <div class="choices">
        <button class="choice" data-mode="network">
          <svg viewBox="0 0 48 48" aria-hidden="true"><rect x="4" y="10" width="16" height="12" rx="2"/><rect x="28" y="26" width="16" height="12" rx="2"/><path d="M12 22v10h16"/></svg>
          <div><strong>Depuis un autre ordinateur</strong>
          <span>Par le Wi-Fi, un câble réseau ou un câble Thunderbolt. Les deux ordinateurs doivent être allumés.</span></div>
        </button>
        <button class="choice" data-mode="disk">
          <svg viewBox="0 0 48 48" aria-hidden="true"><rect x="8" y="12" width="32" height="24" rx="4"/><circle cx="33" cy="30" r="2"/><path d="M14 20h14"/></svg>
          <div><strong>Depuis un disque externe</strong>
          <span>Vous avez déjà préparé un paquet Bernard sur un disque ou une clé USB.</span></div>
        </button>
      </div>
      <p class="form-error banner error" hidden></p>
      <div class="bar"><span class="summary"></span><button data-act="reset" class="quiet">Revenir</button></div>`));
    main.querySelectorAll(".choice").forEach(b => b.addEventListener("click", async () => {
      try { await api("start", { mode: b.dataset.mode }); } catch (e) { showError(e.message); }
    }));
    main.querySelector("[data-act=reset]").addEventListener("click", () => api("reset"));
  },

  network(s) {
    setRail(2);
    const c = s.code || "";
    main.replaceChildren(el(`
      <h1>Reliez l'ancien ordinateur</h1>
      <p class="lead">Les deux ordinateurs doivent être sur le même réseau, ou reliés par un câble.</p>
      <ol class="howto">
        <li><span>Sur l'ancien ordinateur, ouvrez Bernard et choisissez « Ceci est l'ancien ordinateur »,
          puis « Directement au nouvel ordinateur ».</span></li>
        <li><span>Il trouvera cet ordinateur tout seul et vous demandera ce code :</span></li>
      </ol>
      <div class="code" aria-label="Code d'appairage ${esc(c.split("").join(" "))}">
        ${c.slice(0, 3).split("").map(d => `<b>${esc(d)}</b>`).join("")}<span class="gap"></span>
        ${c.slice(3).split("").map(d => `<b>${esc(d)}</b>`).join("")}
      </div>
      <p class="waiting"><span class="pulse"></span> En attente de l'ancien ordinateur…</p>
      <p class="links-hint">Un câble réseau est plus rapide que le Wi-Fi. Un câble USB-C ne fonctionne
      que si les deux ordinateurs ont un port Thunderbolt ou USB4. Vous pourrez changer de liaison pendant le transfert.</p>
      <details><summary>La recherche automatique ne trouve pas cet ordinateur ?</summary>
        <p>Certains Wi-Fi (invités, entreprise) empêchent les appareils de se voir. Reliez les deux ordinateurs
        par un câble. En dernier recours, dans un terminal sur l'ancien ordinateur :</p>
        <p class="addresses">${(s.addresses || []).map(a => `<span class="cmd">sudo bernard-agent connect --target ${esc(a)}</span>`).join("<br>") || "Aucune adresse réseau active."}</p>
      </details>
      <div class="bar"><span class="summary"></span><button data-act="reset" class="quiet">Revenir</button></div>`));
    main.querySelector("[data-act=reset]").addEventListener("click", () => api("reset"));
  },

  disk(s) {
    setRail(2);
    const packs = s.packs || [];
    main.replaceChildren(el(`
      <h1>Ouvrir le paquet de migration</h1>
      <p class="lead">Branchez le disque qui contient le paquet préparé sur l'ancien ordinateur.</p>
      <label class="field">Paquet
        ${packs.length
          ? `<select id="pack">${packs.map(p => `<option>${esc(p)}</option>`).join("")}</select>`
          : `<input id="pack" type="text" placeholder="/media/…/migration">`}
      </label>
      ${packs.length ? "" : `<p class="meta">Aucun paquet trouvé automatiquement : indiquez son dossier.</p>`}
      <label class="field">Phrase de passe du paquet
        <input id="pass" type="password" autocomplete="off">
      </label>
      <p class="form-error banner error" hidden></p>
      <div class="bar"><span class="summary"></span>
        <button data-act="reset" class="quiet">Revenir</button>
        <button data-act="open" class="primary">Ouvrir le paquet</button></div>`));
    main.querySelector("[data-act=reset]").addEventListener("click", () => api("reset"));
    main.querySelector("[data-act=open]").addEventListener("click", async () => {
      try {
        await api("disk", { path: main.querySelector("#pack").value, passphrase: main.querySelector("#pass").value });
      } catch (e) { showError(e.message); }
    });
  },

  analysing(s) {
    setRail(3);
    main.replaceChildren(el(`
      <h1>Analyse de l'ancien ordinateur</h1>
      <p class="lead">${s.peer ? `Connecté à ${esc(s.peer)}. ` : ""}Bernard fait l'inventaire des comptes,
      des applications et des dossiers. Rien n'est encore copié.</p>
      <p class="waiting"><span class="pulse"></span> Lecture en cours…</p>`));
  },

  choose(s) {
    setRail(4);
    const p = s.plan;
    ui.selected = {};
    for (const a of p.actions) ui.selected[a.id] = defaultSelected(a, s);
    const users = p.actions.filter(a => a.op === "createUser" || a.op === "useUser");
    const copies = p.actions.filter(a => a.op === "copy");
    const apps = p.actions.filter(a => ["install", "review", "skip"].includes(a.op) && a.from && a.from.startsWith("a"))
      .sort((a, b) => lastUsed(s, b) - lastUsed(s, a));
    const net = p.actions.filter(a => a.op === "importWifi" || a.op === "addPrinter");
    const sets = p.actions.filter(a => a.op === "settings");
    const autologin = p.actions.filter(a => a.op === "autoLoginOff");
    const sysdata = p.actions.filter(a => a.op === "systemData");
    const repos = p.actions.filter(a => a.op === "addRepo");
    const skipped = apps.filter(a => a.op === "skip");
    const visibleApps = apps.filter(a => a.op !== "skip");
    const removals = p.actions.filter(a => a.op === "remove");
    const keyboards = p.actions.filter(a => a.op === "keyboard");

    main.replaceChildren(el(`
      <h1>Choisissez ce qui vient avec vous</h1>
      <p class="lead">Depuis ${esc(s.source?.hostname || "l'ancien ordinateur")} (${esc(distro(s.source))}).
      Tout est coché par défaut, sauf les applications que vous n'utilisez plus depuis plus d'un an.</p>

      <h2>Comptes</h2>
      <div id="accounts">${users.map(a => accountRow(a, s)).join("")}</div>

      <h2>Dossiers personnels</h2>
      <ul class="list">${copies.map(a => row(a, `Dossier personnel de ${esc(a.login)}`,
        `${bytes(a.bytes)}, ${plural(a.files, "fichier", "fichiers")}. Les caches et la corbeille ne sont pas copiés.`)).join("")}</ul>

      <h2>Réglages</h2>
      <ul class="list">${sets.map(a => row(a, `Réglages de ${esc(a.login)}`, settingsMeta(a, s),
        a.fidelity === "substitute" ? `<span class="tag substitute">Traduits</span>` : a.reason ? `<span class="tag none">Partiel</span>` : `<span class="tag full">Identiques</span>`)).join("")}
      ${autologin.map(a => row(a, `Ne plus ouvrir seule la session « ${esc(a.login)} »`,
        `Au démarrage, cet ordinateur ouvre directement le compte ${esc(a.login)}. Après la migration, l'écran de connexion vous laissera choisir votre compte.`)).join("")}</ul>

      <h2>Applications</h2>
      ${visibleApps.some(a => a.op === "install") ? `<label class="meta"><input type="checkbox" id="all-apps"> Tout sélectionner</label>` : ""}
      ${repos.length ? `<ul class="list">${repos.map(a => row(a, `Dépôt de logiciels ${esc(a.label)}`,
        "Ajouté ici pour réinstaller les logiciels qui en viennent. S'il ne répond pas pour cette version du système, il est retiré aussitôt.",
        `<span class="tag full">Dépôt</span>`)).join("")}</ul>` : ""}
      <ul class="list" id="apps">${visibleApps.map(a => appRow(a, s)).join("") || `<li><span></span><span class="meta">Aucune application à installer : tout est déjà présent.</span><span></span></li>`}</ul>
      ${skipped.length ? `<button class="link toggle-more" id="more">Afficher les ${skipped.length} éléments déjà présents sur cet ordinateur</button>
        <ul class="list" id="skipped" hidden>${skipped.map(a => `<li class="off"><span></span><span><span class="name">${esc(a.label)}</span>
        <span class="meta"> ${skipWhy(a)}</span></span><span></span></li>`).join("")}</ul>` : ""}
      ${removals.length ? `<p class="meta" id="rm-summary"></p>` : ""}

      ${p.trimmed?.length ? `<p class="banner warn">Tout ne tient pas sur ce disque : ${plural(p.trimmed.length, "élément", "éléments")} hors des dossiers personnels
        ${p.trimmed.length > 1 ? "ont été décochés" : "a été décoché"} (${p.trimmed.map(esc).join(", ")}). Vos dossiers personnels passent en priorité ;
        ajustez ci-dessous ce que vous voulez emporter.</p>` : ""}
      ${sysdata.length ? systemDataSection(sysdata) : ""}

      ${net.length ? `<h2>Réseau et imprimantes</h2><ul class="list">${net.map(a => a.op === "importWifi"
        ? row(a, `Wi-Fi « ${esc(a.label)} »`, "Le mot de passe du réseau est repris.")
        : row(a, `Imprimante ${esc(a.label)}`, "Réinstallée automatiquement si c'est une imprimante réseau ; une imprimante USB sera à rebrancher.")).join("")}</ul>` : ""}

      ${advancedSection(s, removals, keyboards)}

      ${(s.warnings || []).map(w => `<p class="banner warn">${esc(w)}</p>`).join("")}
      <p class="form-error banner error" hidden></p>
      <div class="bar">
        <div class="summary"><span id="sum-text"></span><div class="space" id="space"><i></i></div></div>
        <button data-act="reset" class="quiet">Annuler</button>
        <button data-act="go" class="primary">Lancer la migration</button>
      </div>`));

    main.querySelectorAll("input[data-id]").forEach(cb => cb.addEventListener("change", () => {
      ui.selected[cb.dataset.id] = cb.checked;
      cb.closest("li")?.classList.toggle("off", !cb.checked);
      updateSummary(s);
    }));
    main.querySelectorAll("input[data-pw]").forEach(inp => inp.addEventListener("input", () => {
      ui.passwords[inp.dataset.pw + ":" + inp.dataset.n] = inp.value;
    }));
    const all = main.querySelector("#all-apps");
    all?.addEventListener("change", () => {
      main.querySelectorAll("#apps input[data-id]").forEach(cb => {
        cb.checked = all.checked; ui.selected[cb.dataset.id] = all.checked;
        cb.closest("li").classList.toggle("off", !all.checked);
      });
      updateSummary(s);
    });
    main.querySelector("#rm-all")?.addEventListener("change", e => {
      main.querySelectorAll("#removals input[data-id]").forEach(cb => {
        cb.checked = e.target.checked; ui.selected[cb.dataset.id] = cb.checked;
        cb.closest("li").classList.toggle("off", !cb.checked);
      });
      updateSummary(s);
    });
    main.querySelector("#more")?.addEventListener("click", e => {
      const l = main.querySelector("#skipped"); l.hidden = !l.hidden;
      e.target.textContent = l.hidden ? `Afficher les ${skipped.length} éléments déjà présents sur cet ordinateur` : "Masquer les éléments déjà présents";
    });
    main.querySelector("[data-act=reset]").addEventListener("click", async () => {
      if (await confirmBox("Annuler la migration ?", "Rien n'a encore été modifié. Vous reviendrez à l'accueil.", "Annuler la migration", true)) api("reset");
    });
    main.querySelector("[data-act=go]").addEventListener("click", () => submit(s));
    updateSummary(s);
  },

  running(s) {
    setRail(5);
    main.replaceChildren(el(`
      <h1>Migration en cours</h1>
      <p class="lead">Vous pouvez utiliser les deux ordinateurs, mais évitez de modifier des fichiers sur l'ancien.
      Chaque fichier est vérifié avant d'être rangé.</p>
      <div id="link"></div>
      <div class="tide" role="progressbar" aria-valuemin="0" aria-valuemax="100" id="bar"><i></i></div>
      <div class="figures">
        <div><span>Étape</span><b id="f-phase"></b></div>
        <div><span>Copié</span><b id="f-bytes"></b></div>
        <div><span>Débit</span><b id="f-speed"></b></div>
        <div><span>Reste environ</span><b id="f-eta"></b></div>
      </div>
      <p class="current-file" id="f-file"></p>
      <details><summary>Journal détaillé</summary><pre class="log" id="log"></pre></details>
      <div class="bar"><span class="summary">Une coupure n'est pas grave : le transfert reprend là où il s'était arrêté.</span>
        <button data-act="stop" class="danger">Arrêter</button></div>`));
    main.querySelector("[data-act=stop]").addEventListener("click", async () => {
      if (await confirmBox("Arrêter la migration ?", "Ce qui est déjà copié et vérifié est conservé. Pour reprendre, relancez Bernard sur les deux ordinateurs.", "Arrêter", true)) api("stop");
    });
    ui.lostKey = null;
    updateRunning(s);
  },

  report(s) {
    ui.extraKey = JSON.stringify(s.extraAccounts || []);
    setRail(6);
    const r = s.result || {};
    const data = Object.values(r.data || {});
    const files = data.reduce((n, d) => n + d.files + d.alreadyPresent, 0);
    const vol = data.reduce((n, d) => n + d.bytes, 0);
    const errors = data.flatMap(d => d.errors || []);
    const renamed = data.flatMap(d => d.renamed || []);
    const skippedFiles = data.flatMap(d => d.skipped || []);
    const failed = [...Object.entries(r.system?.failed || {}), ...Object.entries(r.settings?.failed || {})];
    const setApplied = r.settings?.applied || [];
    const setSkipped = Object.entries(r.settings?.skipped || {});
    const ok = !errors.length && !failed.length;
    const title = s.undo ? "Migration annulée" : ok ? "Tout est arrivé" : "Migration terminée, quelques éléments à revoir";
    const lead = s.undo ? "Cet ordinateur est revenu à son état d'avant la migration, à l'exception des fichiers modifiés depuis. Voici ce qui avait été fait."
      : ok ? "Chaque fichier a été vérifié. Redémarrez l'ordinateur avant d'utiliser vos applications : mots de passe des navigateurs et réglages du bureau ne sont pris en compte qu'à la prochaine ouverture de session."
      : "Tout le reste a été copié et vérifié. Voici ce qui demande votre attention.";
    main.replaceChildren(el(`
      <h1>${title}</h1>
      <p class="lead">${lead}</p>
      <ul class="results">
        <li>${plural(files, "fichier vérifié", "fichiers vérifiés")} (${bytes(vol)} copiés pendant cette migration)</li>
        ${(r.system?.usersCreated || []).map(u => `<li>Compte ${esc(u)} créé</li>`).join("")}
        ${(r.system?.installed || []).length ? `<li>${plural(r.system.installed.length, "application installée", "applications installées")}</li>` : ""}
        ${(r.system?.removed || []).length ? `<li>Retirées, comme sur l'ancien ordinateur : ${r.system.removed.map(esc).join(", ")}</li>` : ""}
        ${(r.replaced || []).length ? `<li>Trousseau de clés et profils des navigateurs de l'ancien ordinateur mis en place (ceux d'ici sont gardés dans ~/.local/share/bernard/avant-migration)</li>` : ""}
        ${setApplied.map(n => `<li>${esc(n)}</li>`).join("")}
        ${failed.map(([n, why]) => `<li class="bad">${esc(n)} : échec (${esc(why)})</li>`).join("")}
        ${errors.map(e => `<li class="bad">${esc(e.path)} : ${esc(e.error)}</li>`).join("")}
      </ul>
      ${stayed(s)}
      ${setSkipped.length ? `<h2>À faire à la main</h2><ul class="list">${setSkipped.map(([n, why]) =>
        `<li><span></span><span><span class="name">${esc(n)}</span><br><span class="meta">${esc(why.replace(/^non appliqué : /, ""))}</span></span><span></span></li>`).join("")}</ul>` : ""}
      ${renamed.length ? `<h2>Fichiers renommés</h2><p>Un fichier du même nom existait déjà ici ; il a été conservé et la copie a reçu un nouveau nom.</p>
        <ul class="list">${renamed.map(f => `<li><span></span><span class="meta">${esc(f.dst)}</span><span></span></li>`).join("")}</ul>` : ""}
      ${skippedFiles.length ? `<details><summary>${skippedFiles.length} fichiers spéciaux ignorés (tubes, sockets)</summary><pre class="log">${esc(skippedFiles.join("\n"))}</pre></details>` : ""}
      <p class="meta">Rapport détaillé : <span class="cmd">${esc(r.reportPath || "")}</span></p>
      ${s.undo ? "" : extraAccountsSection(s)}
      <div id="undo"></div>
      <div class="bar"><span class="summary">Gardez l'ancien ordinateur intact pour l'instant.</span>
        <button data-act="undo" class="danger">Annuler la migration</button>
        <button data-act="quit" class="quiet">Fermer</button>
        ${s.undo ? "" : `<button data-act="reboot" class="primary">Redémarrer maintenant</button>`}</div>`));
    main.querySelector("[data-act=quit]").addEventListener("click", () => { api("quit"); window.close(); });
    main.querySelectorAll("[data-rm]").forEach(b => b.addEventListener("click", async () => {
      const login = b.dataset.rm, on = b.dataset.on === "1";
      const acc = (s.extraAccounts || []).find(x => x.login === login) || {};
      if (on && !(await confirmBox(`Supprimer le compte « ${login} » ?`,
        `Au prochain démarrage, avant l'écran de connexion, le compte ${login} et son dossier personnel seront supprimés` +
        (acc.files ? ` (${plural(acc.files, "fichier personnel", "fichiers personnels")}, ${bytes(acc.bytes)})` : " (aucun fichier personnel)") +
        `. Cette suppression est définitive. Vous pouvez encore changer d'avis ici tant que l'ordinateur n'a pas redémarré.`,
        "Supprimer au prochain démarrage", true))) return;
      try { await api("remove-account", { login, on }); } catch (e) { alert(e.message); }
    }));
    main.querySelector("[data-act=reboot]")?.addEventListener("click", async () => {
      if (await confirmBox("Redémarrer maintenant ?", "Fermez d'abord vos autres applications. Après le redémarrage, connectez-vous avec votre compte : tout sera en place.", "Redémarrer", false)) {
        try { await api("reboot"); } catch (e) { alert(e.message); }
      }
    });
    main.querySelector("[data-act=undo]").addEventListener("click", async () => {
      if (await confirmBox("Annuler la migration ?",
        "Bernard retirera les fichiers qu'il a copiés (sauf ceux modifiés depuis), les applications et les comptes qu'il a ajoutés. Les fichiers qui étaient déjà sur cet ordinateur ne sont pas touchés.",
        "Annuler la migration", true)) {
        try { await api("undo"); } catch (e) { alert(e.message); }
      }
    });
    renderUndo(s);
  },

  stopped(s) {
    setRail(5);
    main.replaceChildren(el(`
      <h1>Migration interrompue</h1>
      <p class="lead">Ce qui a été copié et vérifié est conservé. Rien ne sera recopié.</p>
      ${s.error ? `<p class="banner error">${esc(s.error)}</p>` : ""}
      <p>Pour reprendre : fermez Bernard, relancez-le ici (« Depuis un autre ordinateur »), puis sur l'ancien
      ordinateur cliquez sur « Reprendre ». La migration continuera là où elle s'était arrêtée.</p>
      <div class="bar"><span class="summary"></span><button data-act="quit" class="primary">Fermer Bernard</button></div>`));
    main.querySelector("[data-act=quit]").addEventListener("click", () => { api("quit"); window.close(); });
  },
};

// ------------------------------------------------------------ écran de choix

function lastUsed(s, a) {
  const t = s.apps?.[a.from];
  return t ? new Date(t).getTime() : Infinity; // inconnue = en tête, cochée
}
const YEAR = 365 * 864e5;
function defaultSelected(a, s) {
  if (!a.selected) return false;
  const t = s.apps?.[a.from];
  if (a.op === "install" && t && Date.now() - new Date(t) > YEAR) return false;
  return true;
}
function row(a, name, meta, tag) {
  const on = ui.selected[a.id];
  return `<li class="${on ? "" : "off"}"><input type="checkbox" data-id="${a.id}" ${on ? "checked" : ""} aria-label="${esc(name)}">
    <span><span class="name">${name}</span><br><span class="meta">${meta}</span></span>${tag || "<span></span>"}</li>`;
}
const DESKTOPS = { gnome: "GNOME", cinnamon: "Cinnamon", kde: "KDE Plasma", xfce: "Xfce", mate: "MATE" };
function settingsMeta(a, s) {
  const src = DESKTOPS[s.source?.desktop], dst = DESKTOPS[s.plan.target.desktop];
  if (a.reason === "desktopMismatch")
    return src && dst
      ? `De ${src} vers ${dst}, les réglages du bureau ne se transposent pas. Les tâches planifiées suivent.`
      : "Bureau non reconnu : les réglages du bureau ne sont pas repris. Les tâches planifiées suivent.";
  if (a.fidelity === "substitute")
    return `Traduits de ${src} vers ${dst} : fond d'écran, clavier, souris, favoris, taille du texte, veille. Tâches planifiées incluses.`;
  if (s.plan.actions.some(x => x.op === "keyboard" && x.login === a.login))
    return "Fond d'écran, souris, dock, polices, veille, terminal. Tâches planifiées incluses. La disposition du clavier d'ici est conservée.";
  return "Fond d'écran, clavier, souris, dock, polices, veille, terminal. Tâches planifiées incluses.";
}
function skipWhy(a) {
  if (a.reason === "snapInfrastructure") return "composant technique, inutile ici";
  if (a.reason === "hardware") return "lié au matériel de l'ancien ordinateur : chaque ordinateur garde ses propres pilotes";
  return "déjà installé";
}
const GPU = { nvidia: "NVIDIA", amd: "AMD", intel: "Intel", autre: "autre" };
function gpuList(g) { return (g || []).map(x => GPU[x] || x).join(" + ") || "inconnue"; }
function removalMeta(a) {
  const why = a.reason === "removedOnSource" ? `Vous l'aviez retirée de l'ancien ordinateur le ${dateFR(a.date)}.`
    : a.reason === "absentOnSource" ? "Absente de l'ancien ordinateur."
    : "Absente de l'ancien ordinateur, dont le système est d'une autre version : elle est peut-être simplement nouvelle ici.";
  const size = a.bytes ? ` Libère ${bytes(a.bytes)}.` : "";
  const also = a.also?.length ? ` Retire aussi : ${a.also.map(esc).join(", ")}.` : "";
  return why + size + also;
}
function advancedSection(s, removals, keyboards) {
  const src = s.source || {}, dst = s.plan.target || {};
  const parts = [];
  if (removals.length) parts.push(`
    <h3>Applications absentes de l'ancien ordinateur</h3>
    <p class="meta">Installées d'office avec ${esc(distro(dst) || "ce système")}, mais que vous n'avez pas sur l'ancien ordinateur.
    Cochées : elles seront retirées d'ici. Vos fichiers ne sont pas touchés, et « Annuler la migration » les réinstalle.</p>
    <label class="meta"><input type="checkbox" id="rm-all"> Tout cocher</label>
    <ul class="list" id="removals">${removals.map(a => row(a, esc(a.label), removalMeta(a),
      `<span class="tag none">${a.via === "flatpak" ? "Flatpak" : "Paquet"}</span>`)).join("")}</ul>`);
  if (keyboards.length) parts.push(`
    <h3>Clavier</h3>
    <p class="meta">Le clavier de cet ordinateur (${esc(dst.keyboard || "inconnu")}) n'est pas celui de l'ancien (${esc(src.keyboard || "inconnu")}).
    La disposition choisie à l'installation est conservée. Cochez seulement si vous utiliserez ici le même clavier qu'avant.</p>
    <ul class="list">${keyboards.map(a => row(a, `Reprendre la disposition de l'ancien ordinateur pour ${esc(a.login)}`,
      `Disposition ${esc(a.date || "inconnue")} au lieu de ${esc(dst.keyboard || "celle d'ici")}.`)).join("")}</ul>`);
  parts.push(`
    <h3>Matériel</h3>
    <p class="meta">Carte graphique ici : ${esc(gpuList(dst.gpus))} ; sur l'ancien : ${esc(gpuList(src.gpus))}.
    Pilotes, micrologiciels et noyau ne sont jamais recopiés ni retirés : chaque ordinateur garde ceux choisis pour lui.
    Les caches graphiques des navigateurs ne sont pas copiés, ils se refont seuls.</p>`);
  return `<details class="advanced" id="advanced"><summary>Options avancées</summary>${parts.join("")}</details>`;
}
function appRow(a, s) {
  const t = s.apps?.[a.from];
  const used = t ? `Dernière utilisation : ${dateFR(t)}, ${since(t)}` : "Date de dernière utilisation inconnue";
  if (a.op === "review") {
    const why = a.reason === "notInTargetRepos" ? "introuvable dans les dépôts de cet ordinateur" : "pas d'équivalent automatique";
    return `<li class="off"><span></span><span><span class="name">${esc(a.label)}</span><br>
      <span class="meta">${why}${a.suggestion ? ` — suggestion : ${esc(a.suggestion)}` : ""}</span></span><span class="tag none">À faire à la main</span></li>`;
  }
  const tag = a.fidelity === "substitute"
    ? `<span class="tag substitute">Équivalent Flathub</span>`
    : `<span class="tag full">${a.via === "flatpak" ? "Flathub" : a.suggestion ? esc(a.suggestion) : "Identique"}</span>`;
  return row(a, esc(a.label), used, tag);
}
function accountRow(a, s) {
  const needs = (s.needPasswords || []).includes(a.login);
  const exists = a.op === "useUser";
  return `<div class="account">
    <input type="checkbox" data-id="${a.id}" ${ui.selected[a.id] ? "checked" : ""} aria-label="Compte ${esc(a.login)}">
    <div><span class="name">${esc(a.login)}</span><br>
    <span class="meta">${exists ? "Ce compte existe déjà ici : les fichiers y seront ajoutés, sans rien écraser."
      : needs ? "Nouveau compte. Choisissez son mot de passe." : "Nouveau compte, avec le même mot de passe qu'avant."}</span>
    ${needs && !exists ? `<div class="pw">
      <label>Mot de passe <input type="password" data-pw="${esc(a.login)}" data-n="1" autocomplete="new-password"></label>
      <label>Confirmation <input type="password" data-pw="${esc(a.login)}" data-n="2" autocomplete="new-password"></label></div>` : ""}
  </div></div>`;
}
function updateSummary(s) {
  const p = s.plan;
  let need = 0, apps = 0, freed = 0, rm = 0;
  const loginOn = {};
  for (const a of p.actions) {
    if ((a.op === "createUser" || a.op === "useUser")) loginOn[a.login] = ui.selected[a.id];
  }
  for (const a of p.actions) {
    if (!ui.selected[a.id]) continue;
    if (a.op === "copy" && loginOn[a.login] !== false) need += a.bytes;
    if (a.op === "systemData") need += a.used || a.bytes || 0;
    if (a.op === "install") apps++;
    if (a.op === "remove") { rm++; freed += a.bytes || 0; }
  }
  const free = p.target.freeBytes + freed;
  const rs = main.querySelector("#rm-summary");
  if (rs) rs.innerHTML = rm
    ? fr(`${plural(rm, "application que vous n'avez pas sur l'ancien ordinateur sera retirée", "applications que vous n'avez pas sur l'ancien ordinateur seront retirées")} d'ici${freed ? ` (${bytes(freed)} libérés)` : ""}. `) + `<button class="link" id="rm-open">Voir et choisir</button>`
    : `Aucune application ne sera retirée d'ici. <button class="link" id="rm-open">Voir les propositions</button>`;
  main.querySelector("#rm-open")?.addEventListener("click", () => {
    const d = main.querySelector("#advanced"); d.open = true; d.scrollIntoView({ behavior: "smooth" });
  });
  const over = need > free * 0.95;
  main.querySelector("#sum-text").textContent = fr(
    `À copier : ${bytes(need)} sur ${bytes(free)} libres. ` +
    (apps ? `${plural(apps, "application", "applications")} à installer.` : "Aucune application à installer."));
  const sp = main.querySelector("#space");
  sp.classList.toggle("over", over);
  sp.querySelector("i").style.width = Math.min(100, free ? need / free * 100 : 100) + "%";
  main.querySelector("[data-act=go]").disabled = over;
}
async function submit(s) {
  const passwords = {};
  for (const login of s.needPasswords || []) {
    const a = ui.passwords[login + ":1"] || "", b = ui.passwords[login + ":2"] || "";
    const act = s.plan.actions.find(x => x.op === "createUser" && x.login === login);
    if (!act || !ui.selected[act.id]) continue;
    if (!a) return showError(`Choisissez un mot de passe pour le compte ${login}.`);
    if (a !== b) return showError(`Les deux mots de passe du compte ${login} diffèrent.`);
    passwords[login] = a;
  }
  // Un compte décoché entraîne ses données.
  const sel = { ...ui.selected };
  const off = new Set(s.plan.actions.filter(a => (a.op === "createUser" || a.op === "useUser") && !sel[a.id]).map(a => a.login));
  for (const a of s.plan.actions) if (a.op === "copy" && off.has(a.login)) sel[a.id] = false;
  try { await api("submit", { selected: sel, passwords }); showError(""); }
  catch (e) { showError(e.message); }
}

// ------------------------------------------------------------ progression

let speedSamples = [];
function updateRunning(s) {
  const p = s.progress || {};
  const pct = p.planned ? Math.min(100, p.bytes / p.planned * 100) : (p.total ? p.files / p.total * 100 : 0);
  const bar = main.querySelector("#bar");
  if (!bar) return;
  bar.querySelector("i").style.width = pct.toFixed(1) + "%";
  bar.setAttribute("aria-valuenow", Math.round(pct));
  main.querySelector("#f-phase").textContent = { system: "Comptes et applications", settings: "Réglages" }[p.phase] || "Copie des fichiers";
  main.querySelector("#f-bytes").textContent = `${bytes(p.bytes || 0)} / ${bytes(p.planned || 0)}`;
  // Débit mesuré sur les 10 dernières secondes ; rien d'affiché tant que
  // la mesure n'est pas significative.
  const speed = speedOf(p.bytes || 0);
  main.querySelector("#f-speed").textContent = p.linkLost ? "en pause" : speed > 1000 ? bytes(speed) + "/s" : "mesure…";
  const rest = speed > 1000 && !p.linkLost ? ((p.planned || 0) - (p.bytes || 0)) / speed : 0;
  main.querySelector("#f-eta").textContent = rest > 0 ? duration(rest) : "—";
  main.querySelector("#f-file").textContent = p.rel || "";
  const lostKey = p.linkLost ? "lost" + (s.code || "") : "";
  if (ui.lostKey !== lostKey) {
    ui.lostKey = lostKey;
    main.querySelector("#link").replaceChildren(p.linkLost
      ? el(`<div class="banner warn"><p>Liaison perdue. Bernard attend l'ancien ordinateur par une autre liaison (câble ou Wi-Fi) ; le transfert reprendra seul.</p>
        ${s.code ? `<p>Si l'envoi a été arrêté sur l'ancien ordinateur, cliquez-y sur « Reprendre » et saisissez ce code :</p>
        <div class="code small">${s.code.slice(0, 3).split("").map(d => `<b>${esc(d)}</b>`).join("")}<span class="gap"></span>${s.code.slice(3).split("").map(d => `<b>${esc(d)}</b>`).join("")}</div>` : ""}</div>`)
      : el(""));
  }
  const log = main.querySelector("#log");
  log.textContent = (s.log || []).join("\n");
}

function updateWait(s) {
  const t = main.querySelector("#w-text");
  if (!t) return;
  const w = s.waited || 0;
  t.textContent = w < 60 ? "Recherche sur le Wi-Fi et les câbles réseau…" : `En attente depuis ${duration(w)}…`;
  main.querySelector("#w-hint").hidden = w < 300;
}

function updateCode(s) {
  const ts = s.targets || [];
  const key = JSON.stringify(ts);
  if (key !== ui.targetsKey) {
    ui.targetsKey = key;
    if (!ts.some(t => t.id === ui.target)) ui.target = ts[0]?.id || "";
    const box = main.querySelector("#targets");
    box.replaceChildren(el(ts.length > 1 ? `<p>Plusieurs nouveaux ordinateurs sont visibles : choisissez le vôtre.</p>
      <div class="choices targets">${ts.map(t => `<label class="choice disk"><input type="radio" name="target" value="${esc(t.id)}" ${t.id === ui.target ? "checked" : ""}>
      <div><strong>${esc(t.name)}</strong><span>par ${esc(t.link)}</span></div></label>`).join("")}</div>`
      : ts.length ? `<p class="found">${ICONS.target}<span><strong>${esc(ts[0].name)}</strong><br><span class="meta">par ${esc(ts[0].link)}</span></span></p>` : ""));
    box.querySelectorAll("input[name=target]").forEach(r => r.addEventListener("change", () => { ui.target = r.value; }));
  }
  main.querySelector("#c-busy").hidden = !s.busy;
  ui.code?.set(!!s.busy);
  const inv = s.inventory;
  main.querySelector("#c-inv").textContent = inv
    ? `Cet ordinateur : ${plural(inv.users, "compte", "comptes")}, ${plural(inv.apps, "application", "applications")}, ${bytes(inv.bytes)} de données.`
    : "Inventaire de cet ordinateur en cours…";
  if (!s.busy && s.error && ui.lastError !== s.error) { ui.lastError = s.error; ui.code?.clear(); }
  if (!s.error) ui.lastError = "";
}

function updateSend(s) {
  const p = s.send || {};
  const disk = !!p.dest;
  const [title, lead] = disk
    ? (p.phase === "prepare" ? PHASES.prepare : ["Écriture du paquet", "Vous pouvez continuer à utiliser cet ordinateur. Ne débranchez pas le disque."])
    : (PHASES[p.phase] || PHASES.copy);
  main.querySelector("#p-title").textContent = fr(title);
  main.querySelector("#p-lead").textContent = fr(lead);
  const copying = p.phase === "copy" || (p.bytes || 0) > 0;
  main.querySelector("#p-copy").hidden = !copying;
  main.querySelector("#p-wait").hidden = copying;
  main.querySelector("#p-wait-text").textContent = p.phase === "system" ? "Installation sur le nouvel ordinateur…" : "Patientez…";
  const planned = p.planned || 0;
  const pct = planned ? Math.min(100, (p.bytes || 0) / planned * 100) : 0;
  const bar = main.querySelector("#bar");
  bar.querySelector("i").style.width = pct.toFixed(1) + "%";
  bar.setAttribute("aria-valuenow", Math.round(pct));
  main.querySelector("#f-bytes").textContent = planned ? `${bytes(p.bytes || 0)} / ${bytes(planned)}` : bytes(p.bytes || 0);
  const speed = copying ? speedOf(p.bytes || 0) : 0;
  main.querySelector("#f-speed").textContent = p.linkLost ? "en pause" : speed > 1000 ? bytes(speed) + "/s" : "mesure…";
  const rest = speed > 1000 && planned && !p.linkLost ? (planned - (p.bytes || 0)) / speed : 0;
  main.querySelector("#f-eta").textContent = rest > 0 ? duration(rest) : "—";
  main.querySelector("#f-link").textContent = p.link ? p.link.charAt(0).toUpperCase() + p.link.slice(1) : "—";
  main.querySelector("#f-file").textContent = p.rel || "";
  main.querySelector("#p-cable").hidden = disk || p.link !== "Wi-Fi";
  main.querySelector("#link").replaceChildren(p.linkLost
    ? el(`<p class="banner warn">Liaison perdue. Bernard cherche le nouvel ordinateur par une autre liaison (câble ou Wi-Fi) ; l'envoi reprendra seul.</p>`)
    : el(""));
  main.querySelector("#log").textContent = (s.log || []).join("\n");
}

const SYSKIND = {
  web: "Sites web", database: "Base de données", container: "Conteneurs", vm: "Machines virtuelles",
  appserver: "Serveur", opt: "Logiciel", srv: "Service", local: "Programmes", etc: "Réglages système",
  root: "Administrateur", service: "Service", custom: "Dossier", disk: "Disque", homeelse: "Dossier personnel",
  backup: "Sauvegarde", steam: "Jeux",
};
const ADVICE = {
  copy: "À reprendre.",
  skip: "Sauvegarde : inutile de la copier si les données qu'elle protège sont déjà migrées.",
  attach: "Autre disque : à brancher tel quel dans le nouvel ordinateur, ou à copier.",
  review: "À examiner : Bernard ne sait pas si c'est utile.",
};
function sysMeta(a) {
  const size = a.bytes ? bytes(a.bytes) : "";
  const files = a.files ? plural(a.files, "fichier", "fichiers") : "";
  const sparse = a.used && a.bytes && a.used < a.bytes * 0.8 ? ` (${bytes(a.used)} réellement occupés : fichiers creux)` : "";
  return [size + sparse, files].filter(Boolean).join(", ") + ". " + (ADVICE[a.suggestion] || "");
}
const LATER = {
  database: "Copie avec arrêt du service : prochaine version.",
  container: "Copie avec arrêt du service : prochaine version.",
  vm: "Copie des disques virtuels avec arrêt des machines : prochaine version.",
  appserver: "Copie avec arrêt du serveur : prochaine version.",
  disk: "Choix de l'emplacement sur le nouvel ordinateur : prochaine version.",
  homeelse: "Choix de l'emplacement sur le nouvel ordinateur : prochaine version.",
  steam: "Choix de l'emplacement sur le nouvel ordinateur : prochaine version.",
  backup: "Choix de l'emplacement sur le nouvel ordinateur : prochaine version.",
};
function systemDataSection(items) {
  const total = items.filter(a => a.suggestion !== "skip").reduce((n, a) => n + (a.used || a.bytes || 0), 0);
  const details = a => a.also?.length ? `<details><summary>${plural(a.also.length, "fichier", "fichiers")}</summary><pre class="log">${esc(a.also.join("\n"))}</pre></details>` : "";
  const tag = a => `<span class="tag ${a.fidelity === "full" ? "full" : "none"}">${esc(SYSKIND[a.reason] || "Données")}</span>`;
  return `<h2>Hors des dossiers personnels</h2>
    <p class="meta">Bernard a examiné tout le disque de l'ancien ordinateur. Voici ce qui n'appartient ni au système
    ni à vos dossiers personnels${total ? ` (environ ${bytes(total)} hors sauvegardes)` : ""}. Chaque élément est copié
    au même endroit, avec ses propriétaires et ses droits ; un fichier déjà présent ici est mis de côté, et
    « Annuler la migration » le remet en place.</p>
    <ul class="list">${items.map(a => a.fidelity === "full"
      ? row(a, esc(a.label), sysMeta(a) + details(a), tag(a))
      : `<li class="off"><span></span><span><span class="name">${esc(a.label)}</span><br>
        <span class="meta">${sysMeta(a)} ${LATER[a.reason] || ""}</span>${details(a)}</span>${tag(a)}</li>`).join("")}</ul>`;
}

function extraAccountsSection(s) {
  const accs = s.extraAccounts || [];
  if (!accs.length) return "";
  const migrated = (s.plan?.actions || []).filter(a => (a.op === "createUser" || a.op === "useUser") && a.selected).map(a => a.login);
  const who = migrated.length ? `votre compte « ${esc(migrated[0])} »` : "votre compte";
  return `<h2>Compte provisoire</h2>
    <p>Ce compte ne vient pas de l'ancien ordinateur : sans doute celui créé pour installer le système.
    Au redémarrage, choisissez ${who} sur l'écran de connexion. Si vous êtes sûr de votre coup, le compte provisoire peut être supprimé au redémarrage ; sinon, gardez-le et
    supprimez-le plus tard depuis Paramètres → Utilisateurs.</p>
    <ul class="list">${accs.map(a => `<li><span></span><span><span class="name">${esc(a.login)}${a.current ? " (session ouverte en ce moment)" : ""}</span><br>
      <span class="meta">${a.files ? `${plural(a.files, "fichier personnel", "fichiers personnels")}, ${bytes(a.bytes)}` : "Aucun fichier personnel"}${a.admin ? " · administrateur" : ""}.
      ${a.scheduled ? "<strong>Sera supprimé au prochain démarrage.</strong>" : ""}</span></span>
      ${a.scheduled ? `<button class="quiet" data-rm="${esc(a.login)}" data-on="0">Garder ce compte</button>`
        : `<button class="danger" data-rm="${esc(a.login)}" data-on="1">Supprimer au prochain démarrage</button>`}</li>`).join("")}</ul>`;
}

function stayed(s) {
  const items = (s.plan?.actions || []).filter(a => a.op === "systemData" && !a.selected);
  if (!items.length || s.undo) return "";
  return `<h2>Resté sur l'ancien ordinateur</h2>
    <p>Ces éléments hors des dossiers personnels n'ont pas été copiés (non cochés, ou copie prévue dans une prochaine version).
    Gardez l'ancien ordinateur tant qu'ils ne sont pas repris.</p>
    <ul class="list">${items.map(a => `<li><span></span><span><span class="name">${esc(a.label)}</span><br>
      <span class="meta">${sysMeta(a)}</span></span><span></span></li>`).join("")}</ul>`;
}

function renderUndo(s) {
  const box = main.querySelector("#undo");
  if (!box) return;
  if (!s.undo) { box.replaceChildren(); return; }
  const u = s.undo;
  box.replaceChildren(el(`<div class="banner good"><strong>Migration annulée.</strong>
    ${plural(u.files, "fichier retiré", "fichiers retirés")}${u.apps?.length ? `, ${u.apps.length} applications retirées` : ""}${u.reinstalled?.length ? `, ${u.reinstalled.length} applications réinstallées` : ""}${u.users?.length ? `, comptes supprimés : ${u.users.map(esc).join(", ")}` : ""}.
    ${u.kept?.length ? `<br>${plural(u.kept.length, "fichier modifié", "fichiers modifiés")} depuis la copie conservé(s).` : ""}
    ${u.errors?.length ? `<br>Non annulé : ${u.errors.map(esc).join(" ; ")}` : ""}</div>`));
  main.querySelector("[data-act=undo]").disabled = true;
}

// ------------------------------------------------------------ boucle

function render(s) {
  state = s;
  if (s.step !== shown) {
    shown = s.step;
    speedSamples = [];
    (screens[s.step] || screens.role)(s);
    main.focus();
  } else if (s.step === "running") {
    updateRunning(s);
  } else if (s.step === "network") {
    const codeNow = [...main.querySelectorAll(".code b")].map(b => b.textContent).join("");
    if (codeNow !== s.code) screens.network(s);
  } else if (s.step === "report" && ((s.undo && !main.querySelector("#undo .banner")) || JSON.stringify(s.extraAccounts || []) !== ui.extraKey)) {
    screens.report(s);
  } else if (s.step === "src-wait") {
    updateWait(s);
  } else if (s.step === "src-code") {
    updateCode(s);
  } else if (s.step === "src-send") {
    updateSend(s);
  } else if (s.step === "src-disk") {
    if (JSON.stringify((s.disks || []).map(d => d.path)) !== ui.disksKey) screens["src-disk"](s);
    else if (s.inventory) main.querySelector("#d-need").textContent = `Il faut environ ${bytes(s.inventory.bytes)} d'espace libre.`;
  }
  if (s.error && s.step !== "stopped") showError(s.error);
}

function connect() {
  const es = new EventSource("/api/events?t=" + encodeURIComponent(TOKEN));
  es.onmessage = e => render(JSON.parse(e.data));
  es.onerror = () => {
    es.close();
    shown = "";
    main.replaceChildren(el(`<h1>Bernard s'est arrêté</h1><p class="lead">Vous pouvez fermer cette fenêtre.</p>`));
    setTimeout(connect, 3000);
  };
}
connect();
