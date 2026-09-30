// HelloCalc UI: a small state machine for a binary-operation calculator.
// Arithmetic is performed by the server (POST /api/calculate); nothing here
// evaluates arbitrary expressions.
(function () {
  "use strict";

  var MAX_DIGITS = 15;
  var SYMBOLS = { "+": "+", "-": "−", "*": "×", "/": "÷", "%": "mod", "^": "^" };
  var KEYMAP = {
    "Enter": "=", "=": "=", "Backspace": "back", "Escape": "clear", "Delete": "clear",
    "c": "clear", "C": "clear", "x": "*", "X": "*", ",": "."
  };

  function isOp(k) {
    return Object.prototype.hasOwnProperty.call(SYMBOLS, k);
  }

  var exprEl = document.getElementById("expression");
  var valueEl = document.getElementById("value");
  var keysEl = document.getElementById("keys");

  var state;
  var queue = []; // keys pressed while a calculation is in flight

  function reset() {
    state = {
      entry: "0",   // text of the operand being typed or the last result
      fresh: true,  // next digit starts a new entry
      left: null,   // left operand once an operator was chosen
      op: null,     // pending operator
      expr: "",     // expression line
      error: null,  // user-visible error message
      busy: false   // waiting for the server
    };
  }

  // Shows at most 12 significant digits; very large or small magnitudes use
  // exponent notation, which Number() parses back when chaining.
  function format(n) {
    var r = Number.parseFloat(n.toPrecision(12));
    var a = Math.abs(r);
    if (a !== 0 && (a >= 1e15 || a < 1e-9)) {
      return r.toExponential();
    }
    return String(r);
  }

  function operand(n) {
    var s = format(n);
    return n < 0 ? "(" + s + ")" : s;
  }

  function pendingExpr() {
    return format(state.left) + " " + SYMBOLS[state.op];
  }

  function render() {
    exprEl.textContent = state.expr || " ";
    valueEl.classList.toggle("error", state.error !== null);
    valueEl.classList.toggle("long", state.error === null && state.entry.length > 11);
    valueEl.textContent = state.error !== null ? state.error : state.entry;

    var ops = keysEl.querySelectorAll(".key.op");
    for (var i = 0; i < ops.length; i++) {
      var k = ops[i].getAttribute("data-key");
      ops[i].classList.toggle("selected", state.op === k && state.fresh && state.error === null);
    }
  }

  function digitCount(s) {
    return s.replace(/[^0-9]/g, "").length;
  }

  function inputDigit(d) {
    if (state.fresh) {
      state.entry = d;
      state.fresh = false;
    } else if (digitCount(state.entry) >= MAX_DIGITS) {
      return;
    } else if (state.entry === "0") {
      state.entry = d;
    } else if (state.entry === "-0") {
      state.entry = "-" + d;
    } else {
      state.entry += d;
    }
  }

  function inputDecimal() {
    if (state.fresh) {
      state.entry = "0.";
      state.fresh = false;
    } else if (state.entry.indexOf(".") === -1) {
      state.entry += ".";
    }
  }

  function toggleSign() {
    if (state.fresh && state.op !== null) {
      state.entry = "-0";
      state.fresh = false;
      return;
    }
    state.entry = state.entry.charAt(0) === "-" ? state.entry.slice(1) : "-" + state.entry;
  }

  function backspace() {
    if (state.fresh) {
      return;
    }
    state.entry = state.entry.slice(0, -1);
    if (state.entry === "" || state.entry === "-") {
      state.entry = "0";
    }
  }

  function calculate(left, op, right) {
    return fetch("/api/calculate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ left: left, operator: op, right: right })
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (!res.ok || typeof body.result !== "number") {
          throw new Error(body.error || "Request failed (" + res.status + ")");
        }
        return body.result;
      });
    }, function () {
      throw new Error("Network error");
    });
  }

  function showError(expr, err) {
    reset();
    state.expr = expr;
    var msg = err.message || "Error";
    state.error = msg.charAt(0).toUpperCase() + msg.slice(1);
  }

  // Evaluates the pending operation. nextOp, if set, becomes the new pending
  // operator applied to the result (chained calculation).
  function evaluate(nextOp) {
    var left = state.left;
    var op = state.op;
    var right = Number(state.entry);
    var expr = format(left) + " " + SYMBOLS[op] + " " + operand(right);

    state.busy = true;
    return calculate(left, op, right).then(function (result) {
      state.busy = false;
      if (nextOp) {
        state.left = result;
        state.op = nextOp;
        state.entry = format(result);
        state.expr = pendingExpr();
      } else {
        state.left = null;
        state.op = null;
        state.entry = format(result);
        state.expr = expr + " =";
      }
      state.fresh = true;
    }, function (err) {
      showError(expr, err);
    }).then(function () {
      render();
      while (queue.length && !state.busy) {
        press(queue.shift());
      }
    });
  }

  function chooseOperator(op) {
    if (op === "-" && state.op === null && state.fresh && state.expr === "") {
      // A leading minus on a cleared calculator starts a negative number.
      state.entry = "-0";
      state.fresh = false;
      return;
    }
    if (state.op !== null && state.fresh) {
      if (op === "-" && state.op !== "-") {
        // "5 × -3": minus right after an operator starts a negative number.
        toggleSign();
        return;
      }
      state.op = op;
      state.expr = pendingExpr();
      return;
    }
    if (state.op !== null) {
      return evaluate(op);
    }
    state.left = Number(state.entry);
    state.op = op;
    state.fresh = true;
    state.expr = pendingExpr();
  }

  function press(key) {
    if (state.busy) {
      queue.push(key);
      return;
    }
    if (state.error !== null) {
      reset();
      if (key === "back" || key === "clear" || key === "=" || isOp(key)) {
        render();
        return;
      }
    }

    var pending;
    if (/^[0-9]$/.test(key)) {
      inputDigit(key);
    } else if (key === ".") {
      inputDecimal();
    } else if (key === "neg") {
      toggleSign();
    } else if (key === "back") {
      backspace();
    } else if (key === "clear") {
      reset();
    } else if (isOp(key)) {
      pending = chooseOperator(key);
    } else if (key === "=") {
      if (state.op !== null && !state.fresh) {
        pending = evaluate(null);
      }
    }
    if (!pending) {
      render();
    }
  }

  function flash(key) {
    var btn = keysEl.querySelector('[data-key="' + key + '"]');
    if (!btn) {
      return;
    }
    btn.classList.add("pressed");
    setTimeout(function () { btn.classList.remove("pressed"); }, 100);
  }

  keysEl.addEventListener("click", function (e) {
    var btn = e.target.closest("button[data-key]");
    if (btn) {
      press(btn.getAttribute("data-key"));
    }
  });

  document.addEventListener("keydown", function (e) {
    if (e.ctrlKey || e.metaKey || e.altKey) {
      return;
    }
    var key = KEYMAP[e.key] || e.key;
    if (!/^[0-9]$/.test(key) && !isOp(key) && [".", "=", "back", "clear"].indexOf(key) === -1) {
      return;
    }
    // Prevents Enter from also activating a focused button and "/" from
    // opening quick-find in some browsers.
    e.preventDefault();
    flash(key);
    press(key);
  });

  // Footer: versions of both services, as reported by the frontend's /version.
  fetch("/version").then(function (res) {
    return res.ok ? res.json() : null;
  }).then(function (info) {
    if (!info || !info.version) {
      return;
    }
    var backend = info.backend && info.backend.version ? "v" + info.backend.version : "unavailable";
    document.getElementById("foot").textContent = "frontend v" + info.version + " \u00b7 backend " + backend;
  }).catch(function () {});

  reset();
  render();
})();
