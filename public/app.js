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
  const elLatency = document.getElementsByClassName("latency")[0];

  const elUploadPopup = document.getElementsByClassName("upload-wrap")[0];
  const elUploadForm = elUploadPopup.getElementsByTagName("form")[0];
  const elUploadFormBusy = elUploadForm.getElementsByClassName("busy")[0];
  const elUploadThrob = elUploadPopup.getElementsByClassName("throb")[0];
  const elUploadErr = elUploadPopup.getElementsByClassName("err")[0];
  const elUploadIF = elUploadPopup.getElementsByClassName("if")[0];

  const elImgPopup = document.getElementsByClassName("img-wrap")[0];
  const elImg = elImgPopup.getElementsByTagName("img")[0];

  const elsPopup = document.querySelectorAll(".popup");
  const elsActions = document.querySelectorAll(".actions .popup-trigger");
  const elsClose = document.querySelectorAll(".popup--close .trigger");

  const elScrollAction = document.querySelector(".actions .scroll");
  const elScrollInfo = elScrollAction.getElementsByTagName("span")[0];

  var status = {
    status: "",
    flash: "",
    err: "",
    flashSince: null,
  };
  var users = [];

  function closePopup(el) { el.style.display = "none"; }
  function openPopup(el) { el.style.display = "block"; }
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

  function imagePopup(src) {
    elImg.src = src;
    openPopup(elImgPopup);
  }

  function updateUsers() {
    var list = [];
    for (var i in users) {
      list.push(users[i].name);
    }

    elUsers.innerText = `Online: ${list.join(", ")}`;
  }
  function maxScroll() {
    return elLog.offsetHeight + elLog.offsetTop - elLogScroll.offsetHeight;
  }

  let newMessages = 0;
  let scrolled = false;
  let lastSize = 0;
  const check = () => {
    const size = elLog.offsetHeight;
    if (size != lastSize) {
      lastSize = size;
      if (!scrolled) {
        elLogScroll.scrollTop = maxScroll();
        newMessages = 0;
        updateScrollInfo();
        return;
      }
    }
    const maxS = maxScroll();
    scrolled = elLogScroll.scrollTop < maxS && maxS > 0;
    if (!scrolled) {
      elLogScroll.scrollTop = maxScroll();
      newMessages = 0;
      updateScrollInfo();
    }
  };

  let wasNewMessages = -1;
  let wasScrolled = !scrolled;
  const updateScrollInfo = () => {
    if (wasNewMessages != newMessages) {
      elScrollInfo.innerText = newMessages;
      if (!newMessages) {
        elScrollInfo.innerText = '';
      }
      wasNewMessages = newMessages;
    }

    if (wasScrolled != scrolled) {
      elScrollAction.style.display = "none";
      if (scrolled) {
        elScrollAction.style.display = "inline-block";
      }
      wasScrolled = scrolled;
    }
  };

  setInterval(() => { requestAnimationFrame(check); }, 50);
  setInterval(updateScrollInfo, 100);
  setInterval(updateStatus, 1000);
  updateStatus();

  // scroll action button
  elScrollAction.addEventListener("click", () => {
    elLogScroll.scrollTop = maxScroll();
  })

  // close buttons of all popups trigger display:none on their parent popup
  for (i in elsClose) {
    if (!elsClose.hasOwnProperty(i)) {
      continue;
    }
    elsClose[i].addEventListener("click", ((el) => {
      return () => { closePopup(el.parentElement.parentElement.parentElement); };
    })(elsClose[i]));
  }

  // click outside of popup is also close
  for (i in elsPopup) {
    if (!elsPopup.hasOwnProperty(i)) {
      continue;
    }
    elsPopup[i].getElementsByClassName("popup--contents")[0].addEventListener("click", (e) => {
      e.stopImmediatePropagation();
    });
    elsPopup[i].addEventListener("click", ((el) => {
      return () => { closePopup(el); };
    })(elsPopup[i]));
  }

  // action buttons with class popup-trigger set display:block on their corresponding popups
  for (i in elsActions) {
    if (!elsActions.hasOwnProperty(i)) {
      continue;
    }
    elsActions[i].addEventListener("click", ((el) => {
      return () => {
        const target = el.getAttribute("data-popup");
        openPopup(document.querySelector(target));
      };
    })(elsActions[i]));
  }

  let uploading;
  // upload trigger
  elUploadForm.addEventListener("submit", (e) => {
    elUploadFormBusy.style.display = "none";
    elUploadErr.innerText = "";
    let elipsis = "";
    uploading = setInterval(() => {
      elUploadThrob.innerText = "uploading" + elipsis;
      elipsis += ".";
      if (elipsis.length > 3) {
        elipsis = "";
      }
    }, 200);
  });

  // uploaded a file
  elUploadIF.addEventListener("load", (e) => {
    elUploadFormBusy.style.display = "block";
    if (uploading) {
      clearInterval(uploading);
      uploading = 0;
    }
    elUploadThrob.innerText = "";
    let el = elUploadIF.contentDocument.getElementsByTagName("body")[0];
    while (el.nodeType !== document.TEXT_NODE) {
      el = el.firstChild;
    }
    const data = JSON.parse(el.textContent);
    if (data.err) {
      elUploadErr.innerText = data.err;
      return;
    }

    window.homechat.chat(data.uri);
    elUploadForm.reset();
    closePopup(elUploadPopup);
  });

  function message(msg) {
    var el = document.createElement("div");
    var tmp = document.createElement("div");
    el.className = "message";
    var fromName = msg.from;
    var date = new Date(msg.stamp).toLocaleString();
    var data = msg.d;
    tmp.innerText = data;
    data = tmp.innerHTML;
    data = data.replace(/(https?:\/\/[^\s<]+)/g,
        (all, sub) => {
          const a = document.createElement("a");
          a.target = "_blank";
          a.href = sub;
          a.innerText = sub;
          if (sub.match(/\.(jpg|jpeg|gif|png|bmp)$/i)) {
            const img = document.createElement("img");
            img.src = sub;
            a.classList.add("img");
            a.innerText = '';
            a.appendChild(img);
          }
          return a.outerHTML;
        });

    el.innerHTML = `<div class="date">${date}</div>
      <div class="bubble">
      <div class="name"></div>
      <div class="msg"></div>
      </div>`;

    el.getElementsByClassName("name")[0].innerText = fromName;
    el.getElementsByClassName("msg")[0].innerHTML = data;

    const imgs = el.querySelectorAll("a.img");
    for (i in imgs) {
      if (!imgs.hasOwnProperty(i)) {
        continue;
      }
      imgs[i].addEventListener("click", (e) => {
        e.preventDefault();
        imagePopup(e.currentTarget.getAttribute("href"));
        return false;
      });
    }

    if (name === fromName) {
      el.classList.add("mine"); // de mine stinkt
    }
    elLog.appendChild(el);

    newMessages++;
  }

  window.homechat.init({
    name: name,
    onName: function (n) {
      name = n;
      elStatus.classList.remove("is-error");
      elStatus.classList.add("is-success");
      status.err = "";
      updateStatus();
    },
    onHistory: function () {
      elLog.innerHTML = '';
    },
    onLatency: function (ms) {
      elLatency.innerText = ms + "ms";
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
    onUsersMessage: function (u) {
      users = u;
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
    homechat.typing();
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
