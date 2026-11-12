// The landing page's browser demo of `pult tui`, the tape counter and the copy
// buttons. Palettes are real `pult palette` output for the eight listed themes.
// The page works without this file: the list and the house colors stay put.

const THEMES = {
  "catppuccin-mocha": { name: "Catppuccin Mocha", bg: "#1e1e2e", fg: "#cdd6f4", ansi: ["#45475a", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#a6adc8", "#585b70", "#f37799", "#89d88b", "#ebd391", "#74a8fc", "#f2aede", "#6bd7ca", "#bac2de"] },
  "dracula": { name: "Dracula", bg: "#282a36", fg: "#f8f8f2", ansi: ["#21222c", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2", "#6272a4", "#ff6e6e", "#69ff94", "#ffffa5", "#d6acff", "#ff92df", "#a4ffff", "#ffffff"] },
  "nord": { name: "Nord", bg: "#2e3440", fg: "#e5e9f0", ansi: ["#2e3440", "#bf616a", "#a3be8c", "#ebcb8b", "#81a1c1", "#b48ead", "#88c0d0", "#e5e9f0", "#4c566a", "#bf616a", "#a3be8c", "#ebcb8b", "#81a1c1", "#b48ead", "#88c0d0", "#8fbcbb"] },
  "gruvbox-dark": { name: "Gruvbox Dark", bg: "#282828", fg: "#ebdbb2", ansi: ["#282828", "#cc241d", "#98971a", "#d79921", "#458588", "#b16286", "#689d6a", "#ebdbb2", "#665c54", "#fb4934", "#b8bb26", "#fabd2f", "#83a598", "#d3869b", "#8ec07c", "#f9f5d7"] },
  "kanagawa": { name: "Kanagawa", bg: "#1f1f28", fg: "#dcd7ba", ansi: ["#1f1f28", "#c34043", "#76946a", "#c0a36e", "#7e9cd8", "#957fb8", "#6a9589", "#dcd7ba", "#54546d", "#c34043", "#76946a", "#c0a36e", "#7e9cd8", "#957fb8", "#6a9589", "#717c7c"] },
  "everforest-dark": { name: "Everforest Dark", bg: "#2d353b", fg: "#d3c6aa", ansi: ["#475258", "#e67e80", "#a7c080", "#dbbc7f", "#7fbbb3", "#d699b6", "#83c092", "#d3c6aa", "#475258", "#e67e80", "#a7c080", "#dbbc7f", "#7fbbb3", "#d699b6", "#83c092", "#d3c6aa"] },
  "ayu-dark": { name: "Ayu Dark", bg: "#0b0e14", fg: "#e6e1cf", ansi: ["#0b0e14", "#f07178", "#aad94c", "#ffb454", "#59c2ff", "#d2a6ff", "#95e6cb", "#e6e1cf", "#3e4b59", "#f26d78", "#7fd962", "#e6b673", "#73b8ff", "#ddbcff", "#39bae6", "#f2f0e7"] },
  "rose-pine-dawn": { name: "Rose Pine Dawn", bg: "#faf4ed", fg: "#575279", ansi: ["#f2e9e1", "#b4637a", "#286983", "#ea9d34", "#56949f", "#907aa9", "#d7827e", "#575279", "#9893a5", "#b4637a", "#286983", "#ea9d34", "#56949f", "#907aa9", "#d7827e", "#575279"] },
};

const paint = (el, theme) => {
  if (theme) {
    el.style.setProperty("--bg", theme.bg);
    el.style.setProperty("--fg", theme.fg);
    theme.ansi.forEach((hex, i) => el.style.setProperty(`--a${i}`, hex));
  } else {
    el.style.removeProperty("--bg");
    el.style.removeProperty("--fg");
    for (let i = 0; i < 16; i++) el.style.removeProperty(`--a${i}`);
  }
};

const list = document.querySelector("[data-rows]");
if (list) {
  const rows = [...list.children];
  const swatches = document.querySelector("[data-swatches]");
  const note = document.querySelector("[data-note]");
  const preview = {
    name: document.querySelector("[data-preview-name]"),
    id: document.querySelector("[data-preview-id]"),
    tags: document.querySelector("[data-preview-tags]"),
  };
  const status = document.querySelector("[data-status]");
  const channel = document.querySelector("[data-channel]");
  const band = document.querySelector(".band");
  const quiet = matchMedia("(prefers-reduced-motion: reduce)");
  const history = [];
  let cursor = 0;
  let onAir = -1; // -1 is the house palette the page loads with

  const label = (i) => `CH ${i < 0 ? "16" : String(i + 1).padStart(2, "0")}`;

  const move = (i) => {
    cursor = (i + rows.length) % rows.length;
    rows.forEach((row, n) => row.setAttribute("aria-selected", String(n === cursor)));
    const row = rows[cursor];
    list.setAttribute("aria-activedescendant", row.id);
    paint(swatches, THEMES[row.dataset.id]);
    preview.name.textContent = THEMES[row.dataset.id].name;
    preview.id.textContent = row.dataset.id;
    preview.tags.textContent = row.dataset.tags;
    note.textContent = `${cursor + 1} of ${rows.length}`;
  };

  const tune = (i, verb) => {
    onAir = i;
    paint(document.documentElement, i < 0 ? null : THEMES[rows[i].dataset.id]);
    channel.textContent = label(i);
    status.textContent = `${label(i)} ${i < 0 ? "HOUSE PALETTE" : THEMES[rows[i].dataset.id].name.toUpperCase()} - ${verb}`;
    if (!quiet.matches) {
      band.classList.remove("slip");
      void band.offsetWidth;
      band.classList.add("slip");
    }
  };

  const apply = () => {
    if (cursor === onAir) return;
    history.push(onAir);
    tune(cursor, "APPLIED");
  };

  const undo = () => {
    if (!history.length) {
      status.textContent = "NOTHING TO UNDO";
      return;
    }
    tune(history.pop(), "UNDONE");
  };

  const actions = { up: () => move(cursor - 1), down: () => move(cursor + 1), apply, undo };

  list.addEventListener("keydown", (e) => {
    const action = { j: "down", ArrowDown: "down", k: "up", ArrowUp: "up", Enter: "apply", u: "undo" }[e.key];
    if (!action || e.ctrlKey || e.metaKey || e.altKey) return;
    e.preventDefault();
    actions[action]();
  });
  // As in the TUI: a click selects a theme, a click on the selected one applies it.
  rows.forEach((row, i) => row.addEventListener("click", () => (i === cursor ? apply() : move(i))));
  document.querySelectorAll("[data-key]").forEach((button) => {
    button.addEventListener("click", () => actions[button.dataset.key]());
  });
  move(0);
}

const counter = document.querySelector("[data-counter]");
if (counter && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
  const start = Date.now();
  const pad = (n) => String(n).padStart(2, "0");
  setInterval(() => {
    const s = 16 + Math.floor((Date.now() - start) / 1000);
    counter.textContent = `SP ${Math.floor(s / 3600)}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`;
  }, 1000);
}

document.querySelectorAll("[data-copy]").forEach((button) => {
  button.hidden = false;
  button.addEventListener("click", async () => {
    const code = document.getElementById(button.dataset.copy).textContent.trim();
    try {
      await navigator.clipboard.writeText(code);
      button.textContent = "COPIED";
    } catch {
      button.textContent = "COPY FAILED";
    }
    setTimeout(() => { button.textContent = "COPY"; }, 1600);
  });
});
