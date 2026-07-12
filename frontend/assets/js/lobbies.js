(() => {
    const list = document.querySelector("#lobby-list");
    const status = document.querySelector("#list-status");
    const pageError = document.querySelector("#page-error");
    const createForm = document.querySelector("#create-form");
    const dialog = document.querySelector("#join-dialog");
    const joinForm = document.querySelector("#join-form");
    const joinName = document.querySelector("#join-name");
    const joinError = document.querySelector("#join-error");
    let selectedLobby;

    document.querySelector("#refresh").addEventListener("click", loadLobbies);
    document.querySelector("#cancel-join").addEventListener("click", () => dialog.close());

    createForm.addEventListener("submit", async (event) => {
        event.preventDefault();
        pageError.textContent = "";
        const data = Object.fromEntries(new FormData(createForm));
        const response = await request("/api/lobbies", {method: "POST", body: JSON.stringify(data)});
        if (response.ok) {
            const lobby = await response.json();
            window.location.assign(`/room/${encodeURIComponent(lobby.id)}`);
            return;
        }
        pageError.textContent = await errorMessage(response);
    });

    joinForm.addEventListener("submit", async (event) => {
        event.preventDefault();
        joinError.textContent = "";
        const password = new FormData(joinForm).get("password");
        const response = await request(`/api/lobbies/${encodeURIComponent(selectedLobby.id)}/join`, {
            method: "POST", body: JSON.stringify({password})
        });
        if (response.ok) {
            window.location.assign(`/room/${encodeURIComponent(selectedLobby.id)}`);
            return;
        }
        joinError.textContent = await errorMessage(response);
    });

    async function loadLobbies() {
        status.hidden = false;
        status.textContent = "Loading…";
        list.replaceChildren();
        try {
            const response = await fetch("/api/lobbies");
            if (!response.ok) throw new Error("Could not load lobbies");
            const {lobbies} = await response.json();
            status.textContent = lobbies.length ? "" : "No lobbies yet. Create the first one.";
            status.hidden = lobbies.length > 0;
            for (const lobby of lobbies) list.append(lobbyItem(lobby));
        } catch (error) {
            status.textContent = error.message;
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
            dialog.showModal();
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
})();
