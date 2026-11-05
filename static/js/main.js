// EDUCORE progressive enhancement (Phase 5).
// Only genuine interactions; every feature also works via plain HTML+server
// validation when this file does not run.
(function () {
  "use strict";

  var body = document.body;

  // ---- Mobile navigation drawer ----
  var openBtn = document.querySelector(".main-nav-toggle");
  if (openBtn && !openBtn.dataset.wired) {
    openBtn.dataset.wired = "1";
    openBtn.addEventListener("click", function (e) {
      e.stopPropagation();
      body.classList.toggle("sidebar-open");
      e.stopPropagation;
    });
  } else if (!openBtn && window.matchMedia("(max-width: 920px)").matches) {
    // Shell pages that pre-date the partial get an injected button.
    var header = document.querySelector(".app-header");
    if (header) {
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "btn btn-secondary main-nav-toggle";
      btn.setAttribute("aria-label", "Open navigation");
      btn.textContent = "\u2630";
      header.insertBefore(btn, header.firstChild);
      btn.addEventListener("click", function (e) {
        e.stopPropagation();
        body.classList.toggle("sidebar-open");
      });
    }
  }
  var closeBtn = document.querySelector(".sidebar-toggle-close");
  if (closeBtn) {
    closeBtn.addEventListener("click", function () {
      body.classList.remove("sidebar-open");
    });
  }
  document.addEventListener("click", function (e) {
    if (!body.classList.contains("sidebar-open")) return;
    if (e.target.closest(".app-sidebar") || e.target.closest(".main-nav-toggle")) return;
    body.classList.remove("sidebar-open");
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") body.classList.remove("sidebar-open");
  });

  // ---- Confirmation dialogs for destructive buttons ----
  document.querySelectorAll("form[data-confirm]").forEach(function (form) {
    form.addEventListener("submit", function (e) {
      var msg = form.getAttribute("data-confirm") || "Are you sure?";
      if (!window.confirm(msg)) e.preventDefault();
    });
  });

  // ---- Auto-dismiss flash notices (progressive only) ----
  document.querySelectorAll("[data-autodismiss]").forEach(function (n) {
    window.setTimeout(function () {
      n.style.transition = "opacity .4s ease";
      n.style.opacity = "0";
      window.setTimeout(function () { n.remove(); }, 420);
    }, 6000);
  });

  // ---- Rubric builder (kept from Phase 3; used by create/edit pages) ----
  var list = document.getElementById("criterionList");
  var orderInput = document.getElementById("criteriaOrder");
  var staticTotal = document.getElementById("rubricTotal");
  if (list && list.dataset.rubric !== "wired") {
    list.dataset.rubric = "wired";
    var totalEl = document.getElementById("rubricTotalValue");
    var seq = 100000; // avoid colliding with existing criterion IDs

    function refresh() {
      var total = 0;
      var order = [];
      list.querySelectorAll(".criterion-row").forEach(function (div) {
        var i = div.dataset.index;
        if (i) order.push(i);
        var v = div.querySelector(".crit-points input, input.crit-points");
        var val = v ? v.value : "";
        total += parseInt(val, 10) || 0;
      });
      if (orderInput) orderInput.value = order.join(",");
      if (totalEl) totalEl.textContent = String(total);
      if (staticTotal) {
        staticTotal.classList.toggle("is-complete", total === 100);
        staticTotal.classList.toggle("is-error", total > 100);
      }
      var pill = list.parentElement.querySelector(".pct-pill");
      if (pill) pill.textContent = total === 100 ? "Ready to publish" : Math.max(0, 100 - total) + " points left";
    }

    function wire(div) {
      var rm = div.querySelector(".criterion-remove");
      if (rm) rm.addEventListener("click", function () { div.remove(); refresh(); });
      var p = div.querySelector("input.crit-points");
      if (p) p.addEventListener("input", refresh);
    }

    var add = document.getElementById("addCriterion");
    if (add && !add.dataset.wired) {
      add.dataset.wired = "1";
      add.addEventListener("click", function () {
        seq++;
        var n = seq;
        var div = document.createElement("div");
        div.className = "criterion-row";
        div.dataset.index = n;
        div.innerHTML =
          '<span class="criterion-num">New</span>' +
          '<div class="criterion-grid">' +
          '<div class="field"><label>Name</label><input name="crit_title_' + n + '" required maxlength="80" placeholder="e.g. Correct algorithm choice"></div>' +
          '<div class="field"><label>Maximum points</label><input class="crit-points" name="crit_points_' + n + '" type="number" min="1" max="100" required></div>' +
          '<div class="field"><label>Topic (optional)</label><input name="crit_topic_' + n + '" maxlength="60"></div>' +
          '<div class="field field-wide"><label>Description</label><input name="crit_description_' + n + '" required maxlength="300" placeholder="What is being evaluated?"></div>' +
          '<div class="field field-wide"><label>Keywords (optional, comma-separated)</label><input name="crit_keywords_' + n + '" maxlength="200" placeholder="token names the DEMO evaluator can match"></div>' +
          "</div>" +
          '<button type="button" class="btn btn-ghost criterion-remove">Remove</button>';
        wire(div);
        list.appendChild(div);
        refresh();
      });
    }
    list.querySelectorAll(".criterion-row").forEach(wire);
    refresh();
  }
})();
