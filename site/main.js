(() => {
  const SVG = "http://www.w3.org/2000/svg";
  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)");

  // A clock that stops while the user has paused or the figure is off screen.
  function makeClock() {
    const clock = { paused: false, hidden: true, waiters: [] };
    clock.running = () => !clock.paused && !clock.hidden;
    clock.update = () => {
      if (!clock.running()) return;
      const waiters = clock.waiters.splice(0);
      waiters.forEach((resolve) => resolve());
    };
    clock.whenRunning = () =>
      clock.running() ? Promise.resolve() : new Promise((resolve) => clock.waiters.push(resolve));
    clock.tween = (duration, step) =>
      new Promise((resolve) => {
        let elapsed = 0;
        let last = null;
        const frame = (now) => {
          if (clock.running()) {
            if (last !== null) elapsed += now - last;
            last = now;
          } else {
            last = null;
          }
          const t = Math.min(elapsed / duration, 1);
          step(t);
          if (t < 1) requestAnimationFrame(frame);
          else resolve();
        };
        requestAnimationFrame(frame);
      });
    clock.sleep = (duration) => clock.tween(duration, () => {});
    return clock;
  }

  function watchVisibility(el, clock) {
    const observer = new IntersectionObserver(([entry]) => {
      clock.hidden = !entry.isIntersecting || document.hidden;
      clock.update();
    });
    observer.observe(el);
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) clock.hidden = true;
      else clock.hidden = el.getBoundingClientRect().bottom < 0 || el.getBoundingClientRect().top > innerHeight;
      clock.update();
    });
  }

  // Draws connectors between elements inside a positioned stage.
  function makeWires(stage, pairs, shape) {
    const svg = stage.querySelector(".wires");
    const paths = new Map();
    pairs.forEach(({ id }) => {
      const path = document.createElementNS(SVG, "path");
      path.dataset.id = id;
      svg.appendChild(path);
      paths.set(id, path);
    });
    const layout = () => {
      const box = stage.getBoundingClientRect();
      svg.setAttribute("viewBox", `0 0 ${box.width} ${box.height}`);
      pairs.forEach(({ id, from, to }) => {
        const a = from.getBoundingClientRect();
        const b = to.getBoundingClientRect();
        const rel = (r) => ({
          l: r.left - box.left, r: r.right - box.left,
          t: r.top - box.top, b: r.bottom - box.top,
          cx: r.left - box.left + r.width / 2, cy: r.top - box.top + r.height / 2,
        });
        paths.get(id).setAttribute("d", shape(rel(a), rel(b)));
      });
    };
    new ResizeObserver(layout).observe(stage);
    layout();
    return { svg, paths, visible: () => getComputedStyle(svg).display !== "none" };
  }

  function horizontal(a, b) {
    const x1 = a.r, y1 = a.cy, x2 = b.l, y2 = b.cy;
    const dx = (x2 - x1) / 2;
    return `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`;
  }

  // Brain on the left of Workers, or stacked above them on narrow screens.
  function orchestraShape(a, b) {
    if (b.l >= a.r - 1) return horizontal(a, b);
    const x = b.l - 14, r = 8;
    return `M${x},${a.b} V${b.cy - r} Q${x},${b.cy} ${x + r},${b.cy} H${b.l}`;
  }

  function packet(clock, wires, id, duration, returning) {
    if (!wires.visible()) return clock.sleep(duration);
    const dot = document.createElementNS(SVG, "circle");
    dot.setAttribute("r", "4.5");
    dot.setAttribute("class", returning ? "pkt ret" : "pkt");
    wires.svg.appendChild(dot);
    return clock
      .tween(duration, (t) => {
        const path = wires.paths.get(id);
        const len = path.getTotalLength();
        const eased = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
        const p = path.getPointAtLength(len * (returning ? 1 - eased : eased));
        dot.setAttribute("cx", p.x);
        dot.setAttribute("cy", p.y);
      })
      .then(() => dot.remove());
  }

  function bindPause(button, clock) {
    if (!button) return;
    button.addEventListener("click", () => {
      clock.paused = !clock.paused;
      button.setAttribute("aria-pressed", String(clock.paused));
      button.textContent = clock.paused ? "Play animation" : "Pause animation";
      clock.update();
    });
  }

  /* Hero: Brain dispatches, Workers report evidence, Brain accepts. */
  function orchestra() {
    const fig = document.getElementById("orchestra");
    if (!fig) return;
    const stage = fig.querySelector(".orch-stage");
    const brain = fig.querySelector('[data-anchor="brain"]');
    const status = fig.querySelector("[data-brain-status]");
    const steps = [...fig.querySelectorAll(".phases li")];
    const workers = [...fig.querySelectorAll(".worker")].map((el) => ({
      el,
      exec: el.querySelector("[data-exec]"),
      state: el.querySelector("[data-state]"),
      bar: el.querySelector(".bar span"),
    }));
    const rotations = [
      ["codex", "claude", "opencode"],
      ["claude", "pi", "codex"],
      ["codex", "grok", "claude"],
    ];
    const order = steps.map((li) => li.dataset.step);

    const setPhase = (name) => {
      fig.dataset.phase = name;
      const at = order.indexOf(name);
      steps.forEach((li, i) => {
        li.classList.toggle("on", i === at);
        li.classList.toggle("past", i < at);
      });
    };
    const setWorker = (w, cls, text, fill) => {
      w.el.classList.remove("running", "reported", "accepted");
      if (cls) w.el.classList.add(cls);
      w.state.textContent = text;
      if (fill !== undefined) w.bar.style.width = `${fill * 100}%`;
    };

    if (reduced.matches) {
      setPhase("accept");
      steps.forEach((li) => li.classList.add("past"));
      status.textContent = "Accepted after review";
      workers.forEach((w) => setWorker(w, "accepted", "accepted", 1));
      return;
    }

    const clock = makeClock();
    watchVisibility(fig, clock);
    bindPause(fig.querySelector("[data-pause]"), clock);
    const wires = makeWires(
      stage,
      workers.map((w, i) => ({ id: `w${i}`, from: brain, to: w.el })),
      orchestraShape,
    );
    const wire = (i, cls) => wires.paths.get(`w${i}`).setAttribute("class", cls || "");

    const run = async (w, i, duration, labels) => {
      await clock.tween(duration, (t) => {
        w.bar.style.width = `${t * 100}%`;
        w.state.textContent = labels[Math.min(Math.floor(t * labels.length), labels.length - 1)];
      });
      setWorker(w, "reported", "reported", 1);
      wire(i, "back");
      await packet(clock, wires, `w${i}`, 700, true);
    };

    const cycle = async (n) => {
      const execs = rotations[n % rotations.length];
      workers.forEach((w, i) => {
        setWorker(w, "", "queued", 0);
        wire(i, "");
        if (w.exec.textContent !== execs[i]) {
          w.exec.classList.remove("swap");
          void w.exec.offsetWidth;
          w.exec.classList.add("swap");
          setTimeout(() => (w.exec.textContent = execs[i]), 240);
        }
      });
      setPhase("plan");
      status.textContent = "Planning three scoped concerns";
      await clock.sleep(1500);

      setPhase("delegate");
      status.textContent = `Spawning Workers on ${[...new Set(execs)].join(", ")}`;
      await Promise.all(
        workers.map(async (w, i) => {
          await clock.sleep(i * 200);
          wire(i, "live");
          await packet(clock, wires, `w${i}`, 800, false);
          setWorker(w, "running", "running", 0);
        }),
      );

      setPhase("execute");
      status.textContent = "Workers checking in";
      const durations = [2600, 3400, 2000];
      let reported = 0;
      await Promise.all(
        workers.map((w, i) =>
          run(w, i, durations[i], ["reading", "editing", "testing"]).then(() => {
            reported += 1;
            setPhase("evidence");
            status.textContent = `Reviewing evidence · ${reported} of ${workers.length}`;
          }),
        ),
      );

      const follow = workers[1];
      status.textContent = "Follow-up sent to ios-regression";
      await clock.sleep(500);
      wire(1, "live");
      await packet(clock, wires, "w1", 700, false);
      setWorker(follow, "running", "follow-up", 0);
      await run(follow, 1, 1500, ["follow-up", "testing"]);
      status.textContent = "Evidence reviewed";
      await clock.sleep(600);

      setPhase("accept");
      status.textContent = "Accepted after review";
      workers.forEach((w, i) => {
        setWorker(w, "accepted", "accepted", 1);
        wire(i, "back");
      });
      await clock.sleep(2800);
    };

    (async () => {
      for (let n = 0; ; n += 1) await cycle(n);
    })();
  }

  /* Switchboard: new Workers use the default executor unless another is requested. */
  function board() {
    const fig = document.getElementById("board");
    if (!fig) return;
    const stage = fig.querySelector(".board-stage");
    const router = fig.querySelector('[data-anchor="router"]');
    const note = fig.querySelector("[data-route-note]");
    const badge = fig.querySelector("[data-default]");
    const tasks = [...fig.querySelectorAll("[data-task]")];
    const execs = new Map([...fig.querySelectorAll("[data-exec]")].map((el) => [el.dataset.exec, el]));
    // [task index, requested executor or null for the default]
    const plan = [[0, null], [1, "claude"], ["set-delegated", "opencode"], [2, null], [3, "grok"]];

    if (reduced.matches) return;

    const clock = makeClock();
    watchVisibility(fig, clock);
    const pairs = [
      ...tasks.map((el, i) => ({ id: `t${i}`, from: el, to: router })),
      ...[...execs].map(([name, el]) => ({ id: `e:${name}`, from: router, to: el })),
    ];
    const wires = makeWires(stage, pairs, horizontal);
    const paint = (cls) => wires.paths.forEach((p) => p.setAttribute("class", cls));
    const setDefault = (name) => {
      execs.get(name).appendChild(badge);
      note.textContent = `default: ${name}`;
    };
    const clear = () => {
      tasks.forEach((t) => t.classList.remove("on"));
      execs.forEach((e) => e.classList.remove("on"));
      paint("faint");
    };

    (async () => {
      for (;;) {
        tasks.forEach((t) => t.classList.remove("done"));
        setDefault("codex");
        let current = "codex";
        for (const [task, requested] of plan) {
          clear();
          if (task === "set-delegated") {
            note.textContent = `set-delegated ${requested}`;
            await clock.sleep(1200);
            current = requested;
            setDefault(current);
            await clock.sleep(1000);
            continue;
          }
          const exec = requested || current;
          tasks[task].classList.add("on");
          wires.paths.get(`t${task}`).setAttribute("class", "live");
          await packet(clock, wires, `t${task}`, 700, false);
          note.textContent = `-executor ${exec === "agent" ? "cursor-agent" : exec}`;
          wires.paths.get(`e:${exec}`).setAttribute("class", "back");
          await packet(clock, wires, `e:${exec}`, 700, false);
          execs.get(exec).classList.add("on");
          await clock.sleep(1500);
          tasks[task].classList.remove("on");
          tasks[task].classList.add("done");
          note.textContent = `default: ${current}`;
          await clock.sleep(400);
        }
        await clock.sleep(1200);
      }
    })();
  }

  function copyButtons() {
    document.querySelectorAll("[data-copy]").forEach((button) => {
      button.addEventListener("click", async () => {
        const text = document.getElementById(button.dataset.copy).textContent;
        try {
          await navigator.clipboard.writeText(text);
          button.textContent = "Copied";
        } catch {
          const range = document.createRange();
          range.selectNodeContents(document.getElementById(button.dataset.copy));
          const selection = getSelection();
          selection.removeAllRanges();
          selection.addRange(range);
          button.textContent = "Selected";
        }
        setTimeout(() => (button.textContent = "Copy"), 1600);
      });
    });
  }

  orchestra();
  board();
  copyButtons();
})();
