function init() {
  if (!window.homechat) {
    setTimeout(init, 50);
    return;
  }

  let name;
  while (true) {
    name = localStorage.getItem("name");
    if (name) {
      break;
    }
    name = prompt("Jo breddaaah or waifu, what is your namu?");
    if (name) {
      localStorage.setItem("name", name);
    }
  }

  const elStatus = document.getElementsByClassName("status")[0];
  const elUsers = document.getElementsByClassName("users")[0];
  const elLogScroll = document.getElementsByClassName("overflow")[0];
  const elLog = document.getElementsByClassName("log")[0];
  const elInput = document.getElementsByClassName("inp")[0];
  function maxScroll() {
    console.log("elLog.offsetHeight", elLog.offsetHeight);
    console.log("elLog.offsetTop", elLog.offsetTop);
    console.log("elLogScroll.offsetHeight", elLogScroll.offsetHeight);
    return elLog.offsetHeight + elLog.offsetTop - elLogScroll.offsetHeight;
  }

  var scrolled = false;
  elLogScroll.onscroll = function (e) {
    var pct = elLogScroll.scrollTop / maxScroll();
    scrolled = pct < 0.999;
  };

  function message(msg) {
    var el = document.createElement("div");
    var tmp = document.createElement("div");
    el.className = "message";
    var fromName = msg.from;
    var date = new Date(msg.stamp).toLocaleString();
    var data = msg.d;
    tmp.innerText = data;
    data = tmp.innerHTML;
    data = data.replace(
      /(https?:\/\/[^\s]+)/g,
      '<a target="_blank" href="$1">$1</a>'
    );

    el.innerHTML = `<div class="date">${date}</div>
    <div class="bubble">
        <div class="name">${fromName}</div>
        <div class="msg">${data}</div>
    </div>`;

    if (name === fromName) {
      el.classList.add("mine");
    }

    elLog.appendChild(el);

    if (!scrolled) {
      elLogScroll.scrollTop = maxScroll();
    }
  }

  var status = {
    status: "",
    flash: "",
    err: "",
    flashSince: null,
  };
  var users = {};

  function updateStatus() {
    let s = `${name} ${status.status}`;
    if (status.flashSince && status.flash) {
      const now = new Date().getTime();
      if (now - status.flashSince.getTime() < 5000) {
        s += ` [${status.flash}]`;
      }
    }
    if (status.err) {
      s += ` ERROR:${status.err}`;
    }
    elStatus.innerText = s;
  }

  function updateUsers() {
    var uniq = {};
    var list = [];
    for (var i in users) {
      for (var j in users[i]) {
        const name = users[i][j].name;
        if (!uniq[name]) {
          uniq[name] = true;
          list.push(users[i][j].name);
        }
      }
    }

    elUsers.innerText = `Online: ${list.sort().join(", ")}`;
  }

  setInterval(updateStatus, 1000);

  window.homechat.init({
    name: name,
    onName: function (n) {
      name = n;
      elStatus.classList.remove("is-error");
      elStatus.classList.add("is-success");
      status.err = "";
      updateStatus();
    },
    onChatMessage: function (msg) {
      message(msg);
    },
    onMusicMessage: function (msg) {
      console.log("onMusicMessage", msg);
    },
    onMusicStateMessage: function (msg) {
      console.log("onMusicStateMessage", msg);
    },
    onUsersMessage: function (msg) {
      users[msg.channel] = msg.users;
      updateUsers();
    },
    onLog: function (logstr) {
      status.status = logstr;
      updateStatus();
    },
    onFlash: function (flashmsg) {
      status.flash = flashmsg;
      status.flashSince = new Date();
      updateStatus();
    },
    onError: function (errstr) {
      status.err = errstr;
      elStatus.classList.remove("is-success");
      elStatus.classList.add("is-error");
      updateStatus();
    },
  });

  window.onkeydown = function (e) {
    if (e.keyCode == 13) {
      e.preventDefault();
      if (!e.shiftKey) {
        window.homechat.chat(elInput.value);
        elInput.value = "";
        return;
      }
      elInput.value = elInput.value + "\n";
    }
  };
}

window.onload = function () {
  init();
};
