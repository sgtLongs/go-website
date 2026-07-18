(() => {
    const appBaseURL = new URL(document.baseURI);
    const appURL = (path) => new URL(path, appBaseURL);
    const list = document.querySelector("#lobby-list");
    const status = document.querySelector("#list-status");
    const createDialog = document.querySelector("#create-dialog");
    const createForm = document.querySelector("#create-form");
    const createError = document.querySelector("#create-error");
    const joinDialog = document.querySelector("#join-dialog");
    const joinForm = document.querySelector("#join-form");
    const joinName = document.querySelector("#join-name");
    const joinError = document.querySelector("#join-error");
    let selectedLobby;

    document.querySelector("#refresh").addEventListener("click", () => loadLobbies());
    document.querySelector("#open-create").addEventListener("click", () => {
        createForm.reset();
        createError.textContent = "";
        createDialog.showModal();
    });
    document.querySelector("#cancel-create").addEventListener("click", () => createDialog.close());
    document.querySelector("#cancel-join").addEventListener("click", () => joinDialog.close());

    createForm.addEventListener("submit", async (event) => {
        event.preventDefault();
        createError.textContent = "";
        const data = Object.fromEntries(new FormData(createForm));
        const response = await request(appURL("api/lobbies"), {method: "POST", body: JSON.stringify(data)});
        if (response.ok) {
            const lobby = await response.json();
            window.location.assign(appURL(`room/${encodeURIComponent(lobby.id)}`).href);
            return;
        }
        createError.textContent = await errorMessage(response);
    });

    joinForm.addEventListener("submit", async (event) => {
        event.preventDefault();
        joinError.textContent = "";
        const password = new FormData(joinForm).get("password");
        const response = await request(appURL(`api/lobbies/${encodeURIComponent(selectedLobby.id)}/join`), {
            method: "POST", body: JSON.stringify({password})
        });
        if (response.ok) {
            window.location.assign(appURL(`room/${encodeURIComponent(selectedLobby.id)}`).href);
            return;
        }
        joinError.textContent = await errorMessage(response);
    });

    async function loadLobbies({silent = false} = {}) {
        if (!silent) {
            status.hidden = false;
            status.textContent = "Loading…";
            list.replaceChildren();
        }
        try {
            const response = await fetch(appURL("api/lobbies"));
            if (!response.ok) throw new Error("Could not load lobbies");
            const {lobbies} = await response.json();
            status.textContent = lobbies.length ? "" : "No lobbies yet. Create the first one.";
            status.hidden = lobbies.length > 0;
            list.replaceChildren();
            for (const lobby of lobbies) list.append(lobbyItem(lobby));
        } catch (error) {
            if (!silent) status.textContent = error.message;
        }
    }

    function lobbyItem(lobby) {
        const item = document.createElement("li");
        const details = document.createElement("div");
        const name = document.createElement("strong");
        const count = document.createElement("span");
        const button = document.createElement("button");
        name.textContent = lobby.name;
        count.textContent = `${lobby.playerCount} ${lobby.playerCount === 1 ? "player" : "players"}`;
        details.append(name, count);
        button.textContent = "Join";
        button.addEventListener("click", () => {
            selectedLobby = lobby;
            joinName.textContent = lobby.name;
            joinError.textContent = "";
            joinForm.reset();
            joinDialog.showModal();
        });
        item.append(details, button);
        return item;
    }

    function request(url, options) {
        return fetch(url, {headers: {"Content-Type": "application/json"}, ...options});
    }

    async function errorMessage(response) {
        try { return (await response.json()).error || "Request failed"; }
        catch { return "Request failed"; }
    }

    loadLobbies();
    window.setInterval(() => loadLobbies({silent: true}), 5000);
})();
