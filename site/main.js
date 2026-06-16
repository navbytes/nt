/* nt landing page — vanilla JS, no dependencies, no build step.
   Three small enhancements, all reduced-motion aware:
     1. copy-to-clipboard for install / terminal command blocks
     2. reveal-on-scroll via IntersectionObserver
     3. an optional typed-terminal effect in the hero
   The page is fully functional with JS disabled; this is progressive
   enhancement only. */
(function () {
  "use strict";

  var prefersReducedMotion = window.matchMedia
    ? window.matchMedia("(prefers-reduced-motion: reduce)").matches
    : false;

  /* ---------------------------------------------------------------
     1. Copy-to-clipboard
     Any [data-copy] button copies the text from the element named by
     its data-copy-target (an id) or its own data-copy attribute.
  --------------------------------------------------------------- */
  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    // Fallback for non-secure contexts / older browsers.
    return new Promise(function (resolve, reject) {
      try {
        var ta = document.createElement("textarea");
        ta.value = text;
        ta.setAttribute("readonly", "");
        ta.style.position = "absolute";
        ta.style.left = "-9999px";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
        resolve();
      } catch (err) {
        reject(err);
      }
    });
  }

  function flashCopied(btn) {
    var label = btn.querySelector("[data-copy-label]");
    var original = label ? label.textContent : null;
    btn.classList.add("is-copied");
    btn.setAttribute("aria-label", "Copied to clipboard");
    if (label) label.textContent = "Copied";
    window.setTimeout(function () {
      btn.classList.remove("is-copied");
      if (label && original !== null) label.textContent = original;
      if (btn.dataset.copyAria) btn.setAttribute("aria-label", btn.dataset.copyAria);
    }, 1800);
  }

  var copyButtons = document.querySelectorAll("[data-copy]");
  Array.prototype.forEach.call(copyButtons, function (btn) {
    // Remember the resting aria-label so we can restore it after the flash.
    if (btn.getAttribute("aria-label")) {
      btn.dataset.copyAria = btn.getAttribute("aria-label");
    }
    btn.addEventListener("click", function () {
      var targetId = btn.getAttribute("data-copy-target");
      var text = "";
      if (targetId) {
        var target = document.getElementById(targetId);
        if (target) text = target.dataset.command || target.textContent;
      }
      if (!text) text = btn.getAttribute("data-copy") || "";
      text = text.trim();
      if (!text) return;
      copyText(text).then(
        function () {
          flashCopied(btn);
        },
        function () {
          btn.setAttribute("aria-label", "Copy failed — select and copy manually");
        }
      );
    });
  });

  /* ---------------------------------------------------------------
     2. Reveal-on-scroll
     Elements with .reveal fade/slide in once. Under reduced motion (or
     without IntersectionObserver) we just show everything immediately.
  --------------------------------------------------------------- */
  var revealEls = document.querySelectorAll(".reveal");
  if (prefersReducedMotion || !("IntersectionObserver" in window)) {
    Array.prototype.forEach.call(revealEls, function (el) {
      el.classList.add("is-visible");
    });
  } else {
    var io = new IntersectionObserver(
      function (entries, observer) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            entry.target.classList.add("is-visible");
            observer.unobserve(entry.target);
          }
        });
      },
      { rootMargin: "0px 0px -10% 0px", threshold: 0.12 }
    );
    Array.prototype.forEach.call(revealEls, function (el) {
      io.observe(el);
    });
  }

  /* ---------------------------------------------------------------
     3. Typed-terminal effect (hero)
     Replays the terminal lines with a typewriter feel. Skipped entirely
     under reduced motion — the static markup is already complete and
     readable, so nothing is hidden from those users.
  --------------------------------------------------------------- */
  var term = document.querySelector("[data-typed]");
  if (term && !prefersReducedMotion && "requestAnimationFrame" in window) {
    var lines = Array.prototype.slice.call(term.querySelectorAll(".term-line"));
    if (lines.length) {
      // Capture each line's final HTML, then blank them out to type back in.
      var plan = lines.map(function (line) {
        return {
          el: line,
          html: line.innerHTML,
          text: line.textContent || "",
          typed: line.hasAttribute("data-type"),
        };
      });
      plan.forEach(function (p) {
        p.el.innerHTML = "";
        p.el.style.visibility = "hidden";
      });

      var caret = document.createElement("span");
      caret.className = "term-caret";
      caret.setAttribute("aria-hidden", "true");

      var li = 0;
      function nextLine() {
        if (li >= plan.length) {
          if (caret.parentNode) caret.parentNode.removeChild(caret);
          return;
        }
        var p = plan[li];
        p.el.style.visibility = "visible";
        if (!p.typed) {
          // Output lines appear at once, after a beat.
          p.el.innerHTML = p.html;
          li += 1;
          window.setTimeout(nextLine, 360);
          return;
        }
        // Prompt/command lines type character by character.
        p.el.appendChild(caret);
        var i = 0;
        var chars = p.text;
        function typeChar() {
          if (i <= chars.length) {
            p.el.textContent = chars.slice(0, i);
            p.el.appendChild(caret);
            i += 1;
            window.setTimeout(typeChar, 34 + Math.random() * 36);
          } else {
            p.el.innerHTML = p.html; // restore highlighted spans
            p.el.appendChild(caret);
            li += 1;
            window.setTimeout(nextLine, 420);
          }
        }
        typeChar();
      }
      // Kick off shortly after load so it reads as a live session.
      window.setTimeout(nextLine, 600);
    }
  }

  /* ---------------------------------------------------------------
     Footer year — tiny touch so the date never goes stale.
  --------------------------------------------------------------- */
  var yearEl = document.getElementById("year");
  if (yearEl) yearEl.textContent = String(new Date().getFullYear());
})();
