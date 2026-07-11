(() => {
    const roomID = decodeURIComponent(window.location.pathname.split("/").pop());
    const roomName = document.querySelector("#room-name");
    const joinPanel = document.querySelector("#join-panel");
    const presencePanel = document.querySelector("#presence-panel");
    const joinForm = document.querySelector("#join-form");
    const displayName = document.querySelector("#display-name");
    const status = document.querySelector("#connection-status");
    const statusDot = document.querySelector("#connection-dot");
    const participantList = document.querySelector("#participant-list");
    const participantCount = document.querySelector("#participant-count");

    const participants = new Map();
    let socket;
    let reconnectTimer;
    let reconnectAttempts = 0;
    let chosenName = "";
    let intentionallyClosed = false;

    roomName.textContent = roomID;
    displayName.value = window.localStorage.getItem("presence-display-name") || "";

    joinForm.addEventListener("submit", (event) => {
        event.preventDefault();
        chosenName = displayName.value.trim();
        if (!chosenName) return;

        window.localStorage.setItem("presence-display-name", chosenName);
        joinPanel.hidden = true;
        presencePanel.hidden = false;
        connect();
    });

    window.addEventListener("beforeunload", () => {
        intentionallyClosed = true;
        window.clearTimeout(reconnectTimer);
        socket?.close();
    });

    function connect() {
        setStatus("Connecting…", false);
        const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
        const url = `${protocol}//${window.location.host}/ws/rooms/${encodeURIComponent(roomID)}?name=${encodeURIComponent(chosenName)}`;
        socket = new WebSocket(url);

        socket.addEventListener("open", () => {
            reconnectAttempts = 0;
            setStatus("Connected", true);
        });

        socket.addEventListener("message", (message) => {
            const event = JSON.parse(message.data);
            if (event.type === "presence_snapshot") {
                participants.clear();
                for (const person of event.data.participants) participants.set(person.id, person);
            } else if (event.type === "user_joined") {
                participants.set(event.data.id, event.data);
            } else if (event.type === "user_left") {
                participants.delete(event.data.id);
            }
            renderParticipants();
        });

        socket.addEventListener("close", () => {
            participants.clear();
            renderParticipants();
            if (!intentionallyClosed) scheduleReconnect();
        });

        socket.addEventListener("error", () => socket.close());
    }

    function scheduleReconnect() {
        const delay = Math.min(1000 * (2 ** reconnectAttempts), 10000);
        reconnectAttempts += 1;
        setStatus(`Disconnected. Reconnecting in ${Math.ceil(delay / 1000)}s…`, false);
        reconnectTimer = window.setTimeout(connect, delay);
    }

    function setStatus(message, connected) {
        status.textContent = message;
        statusDot.classList.toggle("connected", connected);
    }

    function renderParticipants() {
        participantList.replaceChildren();
        for (const person of participants.values()) {
            const item = document.createElement("li");
            item.textContent = person.name;
            participantList.append(item);
        }
        participantCount.textContent = String(participants.size);
    }
})();
