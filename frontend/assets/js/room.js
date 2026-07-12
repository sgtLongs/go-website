(() => {
    const roomID = decodeURIComponent(window.location.pathname.split("/").pop());
    const byID = (id) => document.querySelector(`#${id}`);
    const roomName = byID("room-name");
    const joinPanel = byID("join-panel");
    const presencePanel = byID("presence-panel");
    const joinForm = byID("join-form");
    const displayName = byID("display-name");
    const status = byID("connection-status");
    const statusDot = byID("connection-dot");
    const participantList = byID("participant-list");
    const participantCount = byID("participant-count");
    const gamePanel = byID("game-panel");
    const waitingView = byID("waiting-view");
    const activeView = byID("active-view");
    const endedView = byID("ended-view");
    const startForm = byID("start-form");
    const waitingMessage = byID("waiting-message");
    const nextGameMessage = byID("next-game-message");
    const roleElement = byID("role");
    const roleHelp = byID("role-help");
    const gameError = byID("game-error");

    const participants = new Map();
    let socket;
    let reconnectTimer;
    let reconnectAttempts = 0;
    let chosenName = "";
    let intentionallyClosed = false;
    let isHost = false;
    let playerID = "";
    let role = "";
    let gameState = null;
    let phaseKey = "";
    let submittedProposalVote = false;
    let submittedQuestCard = false;

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

    startForm.addEventListener("submit", (event) => {
        event.preventDefault();
        gameError.textContent = "";
        send({ type: "start_game" });
    });

    byID("quest-team-form").addEventListener("submit", (event) => {
        event.preventDefault();
        const selected = [...document.querySelectorAll('#quest-team-options input:checked')].map((input) => input.value);
        if (selected.length !== 3) {
            gameError.textContent = "Choose exactly three players.";
            return;
        }
        send({ type: "propose_quest", playerIds: selected });
    });
    byID("approve-team").addEventListener("click", () => voteOnProposal(true));
    byID("reject-team").addEventListener("click", () => voteOnProposal(false));
    byID("succeed-quest").addEventListener("click", () => playQuestCard(true));
    byID("fail-quest").addEventListener("click", () => playQuestCard(false));

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
                isHost = event.data.host;
                playerID = event.data.playerId;
                role = event.data.role || "";
                gameState = event.data.game || null;
                renderGame();
            } else if (event.type === "user_joined") {
                participants.set(event.data.id, event.data);
            } else if (event.type === "user_left") {
                participants.delete(event.data.id);
            } else if (event.type === "game_started") {
                role = "";
                setGameState(event.data);
            } else if (event.type === "role_assigned") {
                role = event.data.role;
                renderRole();
                renderPhase();
            } else if (event.type === "game_updated") {
                setGameState(event.data);
            } else if (event.type === "game_cancelled") {
                resetToWaiting(event.data.message);
            } else if (event.type === "error") {
                gameError.textContent = event.data.message;
                submittedProposalVote = false;
                submittedQuestCard = false;
                renderPhase();
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

    function send(command) {
        gameError.textContent = "";
        if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(command));
    }

    function voteOnProposal(choice) {
        if (submittedProposalVote) return;
        submittedProposalVote = true;
        send({ type: "vote_proposal", choice });
        renderPhase();
    }

    function playQuestCard(choice) {
        if (submittedQuestCard) return;
        submittedQuestCard = true;
        send({ type: "play_quest", choice });
        renderPhase();
    }

    function setGameState(nextState) {
        const nextKey = `${nextState.round}:${nextState.phase}:${nextState.captain?.id || ""}`;
        if (nextKey !== phaseKey) {
            submittedProposalVote = false;
            submittedQuestCard = false;
            phaseKey = nextKey;
        }
        gameState = nextState;
        renderGame();
    }

    function renderGame() {
        gamePanel.hidden = false;
        gameError.textContent = "";
        if (!gameState || !gameState.phase) {
            showOnly(waitingView);
            waitingView.append(startForm);
            startForm.hidden = !isHost;
            return;
        }
        if (gameState.phase === "complete") {
            renderEndedGame();
            return;
        }
        showOnly(activeView);
        startForm.hidden = true;
        byID("round-number").textContent = gameState.round;
        byID("captain-name").textContent = gameState.captain.name;
        byID("success-count").textContent = gameState.successfulQuests;
        byID("fail-count").textContent = gameState.failedQuests;
        renderRole();
        renderLastResult();
        renderPhase();
    }

    function renderRole() {
        const isPlayer = gameState?.players?.some((player) => player.id === playerID);
        roleElement.textContent = role || (isPlayer ? "Assigning…" : "Spectator");
        roleHelp.textContent = role === "traitor"
            ? "Stay hidden. You may succeed or fail a quest when selected."
            : role === "innocent"
                ? "Help three quests succeed. You can only play success cards."
                : isPlayer ? "Your role is being dealt." : "A game was already underway when you joined.";
    }

    function renderLastResult() {
        const result = byID("round-result");
        if (gameState.lastQuest) {
            const quest = gameState.lastQuest;
            result.textContent = quest.succeeded
                ? `Round ${quest.round} succeeded: all ${quest.successCards} cards were successes.`
                : `Round ${quest.round} failed: ${quest.failCards} fail card${quest.failCards === 1 ? "" : "s"} revealed.`;
            result.className = `round-result ${quest.succeeded ? "succeeded" : "failed"}`;
            result.hidden = false;
        } else if (gameState.lastProposal && !gameState.lastProposal.approved) {
            result.textContent = `Team rejected (${gameState.lastProposal.yes} yes, ${gameState.lastProposal.no} no). The captain has rotated.`;
            result.className = "round-result failed";
            result.hidden = false;
        } else {
            result.hidden = true;
        }
    }

    function renderPhase() {
        if (!gameState || gameState.phase === "complete") return;
        byID("choosing-view").hidden = gameState.phase !== "choosing_team";
        byID("proposal-view").hidden = gameState.phase !== "voting_on_team";
        byID("quest-view").hidden = gameState.phase !== "playing_quest";
        if (gameState.phase === "choosing_team") renderChoosing();
        if (gameState.phase === "voting_on_team") renderProposal();
        if (gameState.phase === "playing_quest") renderQuest();
    }

    function renderChoosing() {
        const isCaptain = gameState.captain.id === playerID;
        byID("captain-controls").hidden = !isCaptain;
        byID("waiting-for-captain").hidden = isCaptain;
        byID("waiting-for-captain").textContent = `Waiting for ${gameState.captain.name} to choose three players.`;
        if (!isCaptain) return;

        const options = byID("quest-team-options");
        options.replaceChildren();
        for (const player of gameState.players) {
            const label = document.createElement("label");
            label.className = "player-option";
            const input = document.createElement("input");
            input.type = "checkbox";
            input.name = "quest-player";
            input.value = player.id;
            input.addEventListener("change", limitTeamSelection);
            label.append(input, document.createTextNode(player.id === playerID ? `${player.name} (you)` : player.name));
            options.append(label);
        }
    }

    function limitTeamSelection() {
        const choices = [...document.querySelectorAll('#quest-team-options input')];
        const selected = choices.filter((input) => input.checked);
        for (const input of choices) input.disabled = selected.length >= 3 && !input.checked;
    }

    function renderProposal() {
        renderTeam(byID("proposed-team"), gameState.quest);
        const canVote = Boolean(role);
        const controls = byID("proposal-controls");
        controls.hidden = !canVote || submittedProposalVote;
        byID("proposal-progress").textContent = submittedProposalVote
            ? `Vote submitted. Waiting for the others (${gameState.proposalVotesCast}/${gameState.proposalVotesNeeded}).`
            : !canVote
                ? `Waiting for anonymous votes (${gameState.proposalVotesCast}/${gameState.proposalVotesNeeded}).`
                : `${gameState.proposalVotesCast}/${gameState.proposalVotesNeeded} votes submitted.`;
    }

    function renderQuest() {
        renderTeam(byID("quest-team"), gameState.quest);
        const selected = gameState.quest.some((player) => player.id === playerID);
        const controls = byID("quest-controls");
        controls.hidden = !selected || submittedQuestCard;
        byID("fail-quest").hidden = role !== "traitor";
        byID("quest-progress").textContent = submittedQuestCard
            ? `Card submitted. Waiting for the quest team (${gameState.questCardsPlayed}/${gameState.questCardsNeeded}).`
            : !selected
                ? `Waiting for the quest team (${gameState.questCardsPlayed}/${gameState.questCardsNeeded}).`
                : `${gameState.questCardsPlayed}/${gameState.questCardsNeeded} cards submitted.`;
    }

    function renderTeam(list, team) {
        list.replaceChildren();
        for (const player of team) {
            const item = document.createElement("li");
            item.textContent = player.id === playerID ? `${player.name} (you)` : player.name;
            list.append(item);
        }
    }

    function renderEndedGame() {
        showOnly(endedView);
        const innocentsWon = gameState.winner === "innocent";
        byID("winner-message").textContent = innocentsWon ? "The innocents win!" : "The traitor wins!";
        byID("traitor-name").textContent = gameState.traitors.map((player) => player.name).join(", ");
        byID("final-score").textContent = `${gameState.successfulQuests} successful quests · ${gameState.failedQuests} failed quests`;
        startForm.hidden = !isHost;
        if (isHost) endedView.append(startForm);
    }

    function resetToWaiting(message) {
        gameState = null;
        role = "";
        phaseKey = "";
        submittedProposalVote = false;
        submittedQuestCard = false;
        showOnly(waitingView);
        waitingView.append(startForm);
        startForm.hidden = !isHost;
        gameError.textContent = message;
    }

    function showOnly(view) {
        waitingView.hidden = view !== waitingView;
        activeView.hidden = view !== activeView;
        endedView.hidden = view !== endedView;
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
            if (person.host) {
                const badge = document.createElement("span");
                badge.className = "host-badge";
                badge.textContent = "Host";
                item.append(badge);
            }
            participantList.append(item);
        }
        participantCount.textContent = String(participants.size);
        gamePanel.hidden = false;
        if (!gameState) startForm.hidden = !isHost;
        waitingMessage.textContent = isHost ? "Start when at least three players are ready." : "Waiting for the host to start a game.";
        nextGameMessage.textContent = isHost ? "Start a new game when everyone is ready." : "Waiting for the host to start a new game.";
    }
})();
