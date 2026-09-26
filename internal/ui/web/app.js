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
function el(html) {
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return t.content;
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

function setRail(n) {
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

const screens = {
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
      <p class="form-error banner error" hidden></p>`));
    main.querySelectorAll(".choice").forEach(b => b.addEventListener("click", async () => {
      try { await api("start", { mode: b.dataset.mode }); } catch (e) { showError(e.message); }
    }));
  },

  network(s) {
    setRail(2);
    const c = s.code || "";
    main.replaceChildren(el(`
      <h1>Reliez l'ancien ordinateur</h1>
      <p class="lead">Les deux ordinateurs doivent être sur le même réseau, ou reliés par un câble.</p>
      <ol class="howto">
        <li><span>Sur l'ancien ordinateur, ouvrez un terminal et lancez
          <span class="cmd">sudo bernard-agent connect</span></span></li>
        <li><span>Quand il vous le demande, saisissez ce code :</span></li>
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
        par un câble, ou indiquez l'adresse à la main sur l'ancien ordinateur :</p>
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
    const skipped = apps.filter(a => a.op === "skip");
    const visibleApps = apps.filter(a => a.op !== "skip");

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
        a.fidelity === "substitute" ? `<span class="tag substitute">Traduits</span>` : a.reason ? `<span class="tag none">Partiel</span>` : `<span class="tag full">Identiques</span>`)).join("")}</ul>

      <h2>Applications</h2>
      ${visibleApps.some(a => a.op === "install") ? `<label class="meta"><input type="checkbox" id="all-apps"> Tout sélectionner</label>` : ""}
      <ul class="list" id="apps">${visibleApps.map(a => appRow(a, s)).join("") || `<li><span></span><span class="meta">Aucune application à installer : tout est déjà présent.</span><span></span></li>`}</ul>
      ${skipped.length ? `<button class="link toggle-more" id="more">Afficher les ${skipped.length} éléments déjà présents sur cet ordinateur</button>
        <ul class="list" id="skipped" hidden>${skipped.map(a => `<li class="off"><span></span><span><span class="name">${esc(a.label)}</span>
        <span class="meta"> ${a.reason === "snapInfrastructure" ? "composant technique, inutile ici" : "déjà installé"}</span></span><span></span></li>`).join("")}</ul>` : ""}

      ${net.length ? `<h2>Réseau et imprimantes</h2><ul class="list">${net.map(a => a.op === "importWifi"
        ? row(a, `Wi-Fi « ${esc(a.label)} »`, "Le mot de passe du réseau est repris.")
        : row(a, `Imprimante ${esc(a.label)}`, "Réinstallée automatiquement si c'est une imprimante réseau ; une imprimante USB sera à rebrancher.")).join("")}</ul>` : ""}

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
    updateRunning(s);
  },

  report(s) {
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
      : ok ? "Chaque fichier a été vérifié. Vous pouvez fermer votre session et vous reconnecter avec votre compte."
      : "Tout le reste a été copié et vérifié. Voici ce qui demande votre attention.";
    main.replaceChildren(el(`
      <h1>${title}</h1>
      <p class="lead">${lead}</p>
      <ul class="results">
        <li>${plural(files, "fichier vérifié", "fichiers vérifiés")} (${bytes(vol)} copiés pendant cette migration)</li>
        ${(r.system?.usersCreated || []).map(u => `<li>Compte ${esc(u)} créé</li>`).join("")}
        ${(r.system?.installed || []).length ? `<li>${r.system.installed.length} applications installées</li>` : ""}
        ${setApplied.map(n => `<li>${esc(n)}</li>`).join("")}
        ${failed.map(([n, why]) => `<li class="bad">${esc(n)} : échec (${esc(why)})</li>`).join("")}
        ${errors.map(e => `<li class="bad">${esc(e.path)} : ${esc(e.error)}</li>`).join("")}
      </ul>
      ${setSkipped.length ? `<h2>À faire à la main</h2><ul class="list">${setSkipped.map(([n, why]) =>
        `<li><span></span><span><span class="name">${esc(n)}</span><br><span class="meta">${esc(why.replace(/^non appliqué : /, ""))}</span></span><span></span></li>`).join("")}</ul>` : ""}
      ${renamed.length ? `<h2>Fichiers renommés</h2><p>Un fichier du même nom existait déjà ici ; il a été conservé et la copie a reçu un nouveau nom.</p>
        <ul class="list">${renamed.map(f => `<li><span></span><span class="meta">${esc(f.dst)}</span><span></span></li>`).join("")}</ul>` : ""}
      ${skippedFiles.length ? `<details><summary>${skippedFiles.length} fichiers spéciaux ignorés (tubes, sockets)</summary><pre class="log">${esc(skippedFiles.join("\n"))}</pre></details>` : ""}
      <p class="meta">Rapport détaillé : <span class="cmd">${esc(r.reportPath || "")}</span></p>
      <div id="undo"></div>
      <div class="bar"><span class="summary">Conservez l'ancien ordinateur tel quel tant que vous n'avez pas tout vérifié ici.</span>
        <button data-act="undo" class="danger">Annuler la migration</button>
        <button data-act="quit" class="primary">Fermer Bernard</button></div>`));
    main.querySelector("[data-act=quit]").addEventListener("click", () => { api("quit"); window.close(); });
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
      <p>Pour reprendre : fermez Bernard, relancez-le ici, puis relancez <span class="cmd">sudo bernard-agent connect</span>
      sur l'ancien ordinateur. La migration continuera là où elle s'était arrêtée.</p>
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
  return "Fond d'écran, clavier, souris, dock, polices, veille, terminal. Tâches planifiées incluses.";
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
    : `<span class="tag full">${a.via === "flatpak" ? "Flathub" : "Identique"}</span>`;
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
  let need = 0, apps = 0;
  const loginOn = {};
  for (const a of p.actions) {
    if ((a.op === "createUser" || a.op === "useUser")) loginOn[a.login] = ui.selected[a.id];
  }
  for (const a of p.actions) {
    if (!ui.selected[a.id]) continue;
    if (a.op === "copy" && loginOn[a.login] !== false) need += a.bytes;
    if (a.op === "install") apps++;
  }
  const free = p.target.freeBytes;
  const over = need > free * 0.95;
  main.querySelector("#sum-text").textContent =
    `À copier : ${bytes(need)} sur ${bytes(free)} libres. ` +
    (apps ? `${plural(apps, "application", "applications")} à installer.` : "Aucune application à installer.");
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
  const now = Date.now();
  speedSamples.push([now, p.bytes || 0]);
  speedSamples = speedSamples.filter(([t]) => now - t < 10000);
  // Débit mesuré sur les 10 dernières secondes ; rien d'affiché tant que
  // la mesure n'est pas significative.
  const [t0, b0] = speedSamples[0];
  const span = (now - t0) / 1000;
  const speed = span >= 2 ? ((p.bytes || 0) - b0) / span : 0;
  main.querySelector("#f-speed").textContent = speed > 1000 ? bytes(speed) + "/s" : "mesure…";
  const rest = speed > 1000 ? ((p.planned || 0) - (p.bytes || 0)) / speed : 0;
  main.querySelector("#f-eta").textContent = rest > 0 ? duration(rest) : "—";
  main.querySelector("#f-file").textContent = p.rel || "";
  main.querySelector("#link").replaceChildren(p.linkLost
    ? el(`<p class="banner warn">Liaison perdue. Bernard attend l'ancien ordinateur par une autre liaison (câble ou Wi-Fi) ; le transfert reprendra seul.</p>`)
    : el(""));
  const log = main.querySelector("#log");
  log.textContent = (s.log || []).join("\n");
}

function renderUndo(s) {
  const box = main.querySelector("#undo");
  if (!box) return;
  if (!s.undo) { box.replaceChildren(); return; }
  const u = s.undo;
  box.replaceChildren(el(`<div class="banner good"><strong>Migration annulée.</strong>
    ${plural(u.files, "fichier retiré", "fichiers retirés")}${u.apps?.length ? `, ${u.apps.length} applications retirées` : ""}${u.users?.length ? `, comptes supprimés : ${u.users.map(esc).join(", ")}` : ""}.
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
    (screens[s.step] || screens.welcome)(s);
    main.focus();
  } else if (s.step === "running") {
    updateRunning(s);
  } else if (s.step === "network") {
    const codeNow = [...main.querySelectorAll(".code b")].map(b => b.textContent).join("");
    if (codeNow !== s.code) screens.network(s);
  } else if (s.step === "report" && s.undo && !main.querySelector("#undo .banner")) {
    screens.report(s);
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
