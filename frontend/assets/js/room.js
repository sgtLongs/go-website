(() => {
    const roomID = decodeURIComponent(window.location.pathname.split("/").pop());
    const byID = (id) => document.querySelector(`#${id}`);
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
    const roleConfirmation = byID("role-confirmation");
    const roleConfirmationTitle = byID("role-confirmation-title");
    const roleConfirmationHelp = byID("role-confirmation-help");
    const roleWaitingView = byID("role-waiting-view");
    const gameStartingView = byID("game-starting");
    const gameStartingReady = byID("game-starting-ready");
    const mainGameView = byID("main-game-view");
    const questResultAnnouncement = byID("quest-result-announcement");
    const questResultTitle = byID("quest-result-title");
    const questResultDetail = byID("quest-result-detail");
    const questResultAction = byID("quest-result-action");
    const proposalResultAnnouncement = byID("proposal-result-announcement");
    const proposalResultTitle = byID("proposal-result-title");
    const proposalResultDetail = byID("proposal-result-detail");
    const proposalResultCounts = byID("proposal-result-counts");
    const proposalResultAction = byID("proposal-result-action");
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
    let roleConfirmed = false;
    let pendingRoleConfirmations = [];
    let pendingGameStartConfirmations = [];
    let gameStartPlayers = [];
    let gameStarting = false;
    let gameStartConfirmed = false;
    let gameStartCountdownActive = false;
    let gameStartCountdownSeconds = 0;
    let gameStartCountdownTimer;
    let gameState = null;
    let phaseKey = "";
    let submittedProposalVote = false;
    let submittedQuestCard = false;
    let questResultTimer;
    let questResultCountdownTimer;
    let questResultRevealed = false;
    let pendingProposalConfirmations = [];
    let proposalResultConfirmed = false;
    let proposalResultRevealTimer;
    let proposalResultCountdownTimer;
    let deferredQuestResult = null;
    let questTeamSelectionOrder = [];

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
    gameStartingReady.addEventListener("click", () => {
        if (gameStartConfirmed) return;
        gameStartConfirmed = true;
        send({ type: "confirm_game_start" });
        renderGameStarting();
    });

    byID("quest-team-form").addEventListener("submit", (event) => {
        event.preventDefault();
        const selected = [...document.querySelectorAll('#quest-team-options input:checked')].map((input) => input.value);
        if (selected.length !== gameState.questSize) {
            showCaptainSelectionError(selected.length);
            return;
        }
        clearCaptainSelectionError();
        send({ type: "propose_quest", playerIds: selected });
    });
    byID("approve-team").addEventListener("click", () => voteOnProposal(true));
    byID("reject-team").addEventListener("click", () => voteOnProposal(false));
    byID("succeed-quest").addEventListener("click", () => playQuestCard(true));
    byID("fail-quest").addEventListener("click", () => playQuestCard(false));
    roleConfirmation.addEventListener("click", confirmRole);
    roleConfirmation.addEventListener("keydown", (event) => {
        if (event.key !== "Enter" && event.key !== " ") return;
        event.preventDefault();
        confirmRole();
    });
    questResultAnnouncement.addEventListener("click", () => dismissQuestResult());
    questResultAnnouncement.addEventListener("keydown", (event) => {
        if (event.key !== "Enter" && event.key !== " ") return;
        event.preventDefault();
        dismissQuestResult();
    });
    proposalResultAnnouncement.addEventListener("click", confirmProposalResult);
    proposalResultAnnouncement.addEventListener("keydown", (event) => {
        if (event.key !== "Enter" && event.key !== " ") return;
        event.preventDefault();
        confirmProposalResult();
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
                isHost = event.data.host;
                playerID = event.data.playerId;
                role = event.data.role || "";
                gameState = event.data.game || null;
                roleConfirmed = false;
                pendingRoleConfirmations = event.data.pendingRoleConfirmations || [];
                pendingProposalConfirmations = event.data.pendingProposalConfirmations || [];
                gameStarting = Boolean(event.data.gameStarting);
                pendingGameStartConfirmations = event.data.pendingGameStartConfirmations || [];
                gameStartPlayers = event.data.gameStartPlayers || [];
                gameStartConfirmed = gameStarting && !pendingGameStartConfirmations.some((player) => player.id === playerID);
                renderGame();
                renderGameStarting();
                if (pendingProposalConfirmations.length && gameState?.lastProposal) announceProposalResult(gameState.lastProposal);
            } else if (event.type === "user_joined") {
                participants.set(event.data.id, event.data);
            } else if (event.type === "user_left") {
                participants.delete(event.data.id);
            } else if (event.type === "game_started") {
                gameStarting = false;
                gameStartConfirmed = false;
                pendingGameStartConfirmations = [];
                gameStartPlayers = [];
                startGameCountdown();
                role = "";
                roleConfirmed = false;
                pendingRoleConfirmations = event.data.players || [];
                setGameState(event.data);
            } else if (event.type === "role_assigned") {
                role = event.data.role;
                renderRole();
                renderPhase();
                renderRoleConfirmation();
            } else if (event.type === "game_starting") {
                gameStarting = true;
                gameStartConfirmed = false;
                pendingGameStartConfirmations = event.data.pendingPlayers || [];
                gameStartPlayers = event.data.players || [];
                renderGameStarting();
            } else if (event.type === "game_start_confirmations_updated") {
                pendingGameStartConfirmations = event.data.pendingPlayers || [];
                renderGameStarting();
            } else if (event.type === "game_start_cancelled") {
                gameStarting = false;
                gameStartConfirmed = false;
                pendingGameStartConfirmations = [];
                gameStartPlayers = [];
                renderGameStarting();
                gameError.textContent = event.data.message;
            } else if (event.type === "game_updated") {
                setGameState(event.data);
            } else if (event.type === "role_confirmations_updated") {
                pendingRoleConfirmations = event.data.pendingPlayers || [];
                renderGame();
            } else if (event.type === "proposal_result_confirmations_updated") {
                pendingProposalConfirmations = event.data.pendingPlayers || [];
                if (!event.data.waiting || pendingProposalConfirmations.length === 0) dismissProposalResult();
                else updateProposalResultWaitingMessage();
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
        const previousQuestResult = gameState?.lastQuest?.round;
        const previousProposalResult = gameState?.lastProposal;
        if (nextState.phase === "complete") {
            deferredQuestResult = null;
            dismissProposalResult();
            if (gameState?.phase !== "complete") {
                dismissQuestResult(true);
                announceWinnerCountdown();
            }
        } else {
            if (nextState.lastProposal && !previousProposalResult) {
                pendingProposalConfirmations = nextState.players || [];
                announceProposalResult(nextState.lastProposal, nextState);
            }
            if (nextState.lastQuest && nextState.lastQuest.round !== previousQuestResult) {
                if (nextState.lastProposal && !previousProposalResult) deferredQuestResult = nextState.lastQuest;
                else announceQuestResult(nextState.lastQuest);
            }
        }
        const nextKey = `${nextState.round}:${nextState.phase}:${nextState.captain?.id || ""}`;
        if (nextKey !== phaseKey) {
            submittedProposalVote = false;
            submittedQuestCard = false;
            questTeamSelectionOrder = [];
            clearCaptainSelectionError();
            phaseKey = nextKey;
        }
        gameState = nextState;
        renderGame();
    }

    function announceProposalResult(result, state = gameState) {
        window.clearTimeout(proposalResultRevealTimer);
        window.clearInterval(proposalResultCountdownTimer);
        proposalResultConfirmed = false;
        proposalResultAnnouncement.className = "proposal-result-announcement counting-down";
        proposalResultAnnouncement.setAttribute("role", "status");
        proposalResultAnnouncement.removeAttribute("tabindex");
        proposalResultCounts.hidden = true;
        proposalResultDetail.textContent = "All players have voted. Revealing the team vote…";
        proposalResultAction.textContent = "The result will be revealed after the countdown";
        proposalResultAnnouncement.hidden = false;

        let secondsRemaining = 3;
        proposalResultTitle.textContent = String(secondsRemaining);
        proposalResultCountdownTimer = window.setInterval(() => {
            secondsRemaining -= 1;
            if (secondsRemaining > 0) {
                proposalResultTitle.textContent = String(secondsRemaining);
                return;
            }
            window.clearInterval(proposalResultCountdownTimer);
            revealProposalResult(result, state);
        }, 1000);
    }

    function revealProposalResult(result, state) {
        proposalResultAnnouncement.classList.remove("counting-down");
        proposalResultAnnouncement.classList.add(result.approved ? "accepted" : "rejected");
        proposalResultAnnouncement.setAttribute("role", "button");
        proposalResultAnnouncement.setAttribute("tabindex", "0");
        proposalResultAnnouncement.focus();
        proposalResultTitle.textContent = result.approved ? "Team accepted" : "Team rejected";
        proposalResultDetail.textContent = result.approved
            ? "The selected players will go on the quest."
            : "The captain will rotate and a new team must be chosen.";
        byID("proposal-accept-count").textContent = result.yes;
        byID("proposal-reject-count").textContent = result.no;
        const failureLimit = state.proposalRejectLimit || 5;
        const failures = state.lastQuest?.automatic ? failureLimit : state.rejectedProposals || 0;
        renderVoteFailureTrackerFor(byID("proposal-vote-failure-tracker"), failures, failureLimit);
        proposalResultCounts.hidden = false;
        startProposalAutoConfirmCountdown(20);
    }

    function startProposalAutoConfirmCountdown(secondsRemaining) {
        updateProposalResultWaitingMessage(secondsRemaining);
        proposalResultRevealTimer = window.setInterval(() => {
            secondsRemaining -= 1;
            updateProposalResultWaitingMessage(secondsRemaining);
            if (secondsRemaining <= 0) {
                window.clearInterval(proposalResultRevealTimer);
                confirmProposalResult();
            }
        }, 1000);
    }

    function confirmProposalResult() {
        if (proposalResultAnnouncement.classList.contains("counting-down") || proposalResultConfirmed) return;
        proposalResultConfirmed = true;
        window.clearInterval(proposalResultRevealTimer);
        send({ type: "confirm_proposal_result" });
        updateProposalResultWaitingMessage();
    }

    function updateProposalResultWaitingMessage(secondsRemaining) {
        if (proposalResultConfirmed) {
            const others = pendingProposalConfirmations.filter((player) => player.id !== playerID).length;
            proposalResultAction.textContent = others
                ? `Waiting for ${others} other player${others === 1 ? "" : "s"}`
                : "Waiting for the room";
            return;
        }
        proposalResultAction.textContent = Number.isFinite(secondsRemaining)
            ? `Click anywhere to continue · continuing automatically in ${secondsRemaining} second${secondsRemaining === 1 ? "" : "s"}`
            : "Click anywhere to continue";
    }

    function dismissProposalResult() {
        window.clearInterval(proposalResultCountdownTimer);
        window.clearInterval(proposalResultRevealTimer);
        proposalResultAnnouncement.hidden = true;
        if (deferredQuestResult) {
            const quest = deferredQuestResult;
            deferredQuestResult = null;
            announceQuestResult(quest);
        }
    }

    function announceQuestResult(quest) {
        window.clearInterval(questResultTimer);
        window.clearInterval(questResultCountdownTimer);
        questResultRevealed = false;
        questResultAnnouncement.setAttribute("role", "button");
        questResultAnnouncement.setAttribute("tabindex", "0");
        questResultAnnouncement.querySelector(".eyebrow").textContent = "Quest result";
        questResultAnnouncement.classList.remove("succeeded", "failed");
        questResultAnnouncement.classList.add("counting-down");
        questResultDetail.textContent = "All quest cards have been submitted. The result is being revealed…";
        questResultAction.textContent = "Please wait for the countdown";
        questResultAnnouncement.hidden = false;
        questResultAnnouncement.focus();

        let secondsRemaining = 3;
        questResultTitle.textContent = String(secondsRemaining);
        questResultCountdownTimer = window.setInterval(() => {
            secondsRemaining -= 1;
            if (secondsRemaining > 0) {
                questResultTitle.textContent = String(secondsRemaining);
                return;
            }
            window.clearInterval(questResultCountdownTimer);
            revealQuestResult(quest);
        }, 1000);
    }

    function announceWinnerCountdown() {
        window.clearInterval(questResultTimer);
        window.clearInterval(questResultCountdownTimer);
        questResultRevealed = false;
        questResultAnnouncement.setAttribute("role", "status");
        questResultAnnouncement.removeAttribute("tabindex");
        questResultAnnouncement.querySelector(".eyebrow").textContent = "Game over";
        questResultAnnouncement.classList.remove("succeeded", "failed");
        questResultAnnouncement.classList.add("counting-down");
        questResultDetail.textContent = "The game is over. Revealing the winner…";
        questResultAction.textContent = "The winner will be revealed after the countdown";
        questResultAnnouncement.hidden = false;

        let secondsRemaining = 3;
        questResultTitle.textContent = String(secondsRemaining);
        questResultCountdownTimer = window.setInterval(() => {
            secondsRemaining -= 1;
            if (secondsRemaining > 0) {
                questResultTitle.textContent = String(secondsRemaining);
                return;
            }
            window.clearInterval(questResultCountdownTimer);
            dismissQuestResult(true);
        }, 1000);
    }

    function revealQuestResult(quest) {
        const succeeded = quest.succeeded;
        const totalCards = quest.successCards + quest.failCards;
        questResultRevealed = true;
        questResultAnnouncement.classList.remove("counting-down");
        questResultAnnouncement.classList.toggle("succeeded", succeeded);
        questResultAnnouncement.classList.toggle("failed", !succeeded);
        questResultTitle.textContent = succeeded ? "Quest succeeded!" : "Quest failed!";
        questResultDetail.textContent = quest.automatic
            ? `Quest ${quest.round} automatically failed after five rejected teams.`
            : succeeded
                ? `All ${quest.successCards} cards were successes.`
                : `${quest.failCards} out of ${totalCards} cards failed.`;
        let secondsRemaining = 10;
        updateQuestResultAction(secondsRemaining);
        questResultTimer = window.setInterval(() => {
            secondsRemaining -= 1;
            if (secondsRemaining <= 0) {
                window.clearInterval(questResultTimer);
                dismissQuestResult();
                return;
            }
            updateQuestResultAction(secondsRemaining);
        }, 1000);
    }

    function updateQuestResultAction(secondsRemaining) {
        questResultAction.textContent = `Click anywhere to continue · continuing automatically in ${secondsRemaining} second${secondsRemaining === 1 ? "" : "s"}`;
    }

    function dismissQuestResult(force = false) {
        if (!questResultRevealed && !force) return;
        window.clearInterval(questResultTimer);
        window.clearInterval(questResultCountdownTimer);
        questResultAnnouncement.hidden = true;
    }

    function renderGame() {
        gamePanel.hidden = false;
        gameError.textContent = "";
        renderRoleConfirmation();
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
        renderRoleWaiting();
        startForm.hidden = true;
        renderQuestCards(byID("quest-cards"));
        renderVoteFailureTracker();
        renderRole();
        renderLastResult();
        renderPhase();
    }

    function renderGameStarting() {
        const shouldShow = gameStarting || gameStartCountdownActive;
        gameStartingView.hidden = !shouldShow;
        if (!shouldShow) return;

        if (gameStartCountdownActive) {
            byID("game-starting-title").textContent = String(gameStartCountdownSeconds);
            byID("game-starting-players").hidden = true;
            gameStartingReady.hidden = true;
            byID("game-starting-status").textContent = "Roles are being dealt…";
            return;
        }

        byID("game-starting-title").textContent = "Game starting";
        byID("game-starting-players").hidden = false;
        gameStartingReady.hidden = false;
        const pendingIDs = new Set(pendingGameStartConfirmations.map((player) => player.id));
        const list = byID("game-starting-players");
        list.replaceChildren();
        for (const player of gameStartPlayers) {
            const item = document.createElement("li");
            const ready = !pendingIDs.has(player.id);
            item.classList.toggle("ready", ready);
            item.textContent = `${player.name}${player.id === playerID ? " (you)" : ""} · ${ready ? "Ready" : "Waiting"}`;
            list.append(item);
        }
        gameStartingReady.disabled = gameStartConfirmed;
        gameStartingReady.textContent = gameStartConfirmed ? "Ready!" : "Ready up";
        const remaining = pendingGameStartConfirmations.length;
        byID("game-starting-status").textContent = remaining
            ? `Waiting for ${remaining} player${remaining === 1 ? "" : "s"}.`
            : "Everyone is ready. Dealing roles…";
    }

    function startGameCountdown() {
        window.clearInterval(gameStartCountdownTimer);
        gameStartCountdownActive = true;
        gameStartCountdownSeconds = 3;
        renderGameStarting();
        gameStartCountdownTimer = window.setInterval(() => {
            gameStartCountdownSeconds -= 1;
            if (gameStartCountdownSeconds <= 0) {
                window.clearInterval(gameStartCountdownTimer);
                gameStartCountdownActive = false;
                renderGameStarting();
                renderRoleConfirmation();
                return;
            }
            renderGameStarting();
        }, 1000);
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

    function renderRoleConfirmation() {
        const shouldShow = Boolean(role) && !roleConfirmed && !gameStartCountdownActive && gameState?.phase !== "complete";
        const wasHidden = roleConfirmation.hidden;
        roleConfirmation.hidden = !shouldShow;
        document.body.classList.toggle("confirming-role", shouldShow);
        if (!shouldShow) return;

        roleConfirmationTitle.textContent = role;
        roleConfirmationHelp.textContent = role === "traitor"
            ? "Stay hidden. You may succeed or fail a quest when selected."
            : "Help three quests succeed. You can only play success cards.";
        roleConfirmation.classList.toggle("traitor", role === "traitor");
        if (wasHidden) roleConfirmation.focus();
    }

    function confirmRole() {
        if (roleConfirmation.hidden) return;
        roleConfirmed = true;
        send({ type: "confirm_role" });
        renderRoleConfirmation();
        renderRoleWaiting();
    }

    function renderRoleWaiting() {
        const waiting = pendingRoleConfirmations.length > 0;
        roleWaitingView.hidden = !waiting;
        mainGameView.hidden = waiting;
        const list = byID("role-confirmation-players");
        list.replaceChildren();
        for (const player of pendingRoleConfirmations) {
            const item = document.createElement("li");
            item.className = "quest-player-tile waiting";
            const name = document.createElement("strong");
            name.textContent = player.id === playerID ? `${player.name} (you)` : player.name;
            const state = document.createElement("span");
            state.textContent = "Reading role…";
            item.append(name, state);
            list.append(item);
        }
    }

    function renderLastResult() {
        const result = byID("round-result");
        if (gameState.lastQuest) {
            const quest = gameState.lastQuest;
            result.textContent = quest.automatic
                ? `Round ${quest.round} automatically failed after five rejected teams.`
                : quest.succeeded
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
        byID("required-team-size").textContent = gameState.questSize;
        byID("captain-controls").hidden = !isCaptain;
        byID("waiting-for-captain").hidden = isCaptain;
        const waitingMessage = byID("waiting-for-captain");
        const captainName = document.createElement("strong");
        captainName.textContent = gameState.captain.name;
        waitingMessage.replaceChildren(
            document.createTextNode("Waiting for captain "),
            captainName,
            document.createTextNode(` to choose ${gameState.questSize} players.`),
        );
        if (!isCaptain) return;

        renderCaptainQuestCards();
        renderVoteFailureTrackerFor(byID("captain-vote-failure-tracker"));
        questTeamSelectionOrder = questTeamSelectionOrder.filter((id) => gameState.players.some((player) => player.id === id));
        const options = byID("quest-team-options");
        options.replaceChildren();
        for (const player of gameState.players) {
            const label = document.createElement("label");
            label.className = "player-option";
            const input = document.createElement("input");
            input.type = "checkbox";
            input.name = "quest-player";
            input.value = player.id;
            input.checked = questTeamSelectionOrder.includes(player.id);
            input.addEventListener("change", updateTeamSelection);
            const name = document.createElement("span");
            name.textContent = player.id === playerID ? `${player.name} (you)` : player.name;
            label.append(input, name);
            options.append(label);
        }
    }

    function renderCaptainQuestCards() {
        const list = byID("captain-quest-cards");
        const resultsByRound = new Map((gameState.questResults || []).map((result) => [result.round, result]));
        list.replaceChildren();
        for (let round = 1; round <= gameState.totalRounds; round++) {
            const result = resultsByRound.get(round);
            const status = result ? (result.succeeded ? "succeeded" : "failed") : "pending";
            const item = document.createElement("li");
            item.className = status;
            const statusIcon = document.createElement("span");
            statusIcon.textContent = `Quest ${round}`;
            const teamSize = document.createElement("small");
            const requiredPlayers = gameState.questSizes?.[round - 1] || 0;
            teamSize.textContent = `${requiredPlayers} player${requiredPlayers === 1 ? "" : "s"}`;
            item.append(statusIcon, teamSize);
            item.setAttribute("aria-label", `Quest ${round}: ${requiredPlayers} players required; ${status === "pending" ? "not played" : status}`);
            list.append(item);
        }
    }

    function updateTeamSelection(event) {
        const input = event.currentTarget;
        if (input.checked) {
            if (questTeamSelectionOrder.length >= gameState.questSize) {
                const replacedID = questTeamSelectionOrder.pop();
                const replacedInput = document.querySelector(`#quest-team-options input[value="${CSS.escape(replacedID)}"]`);
                if (replacedInput) replacedInput.checked = false;
            }
            questTeamSelectionOrder = questTeamSelectionOrder.filter((id) => id !== input.value);
            questTeamSelectionOrder.push(input.value);
        } else {
            questTeamSelectionOrder = questTeamSelectionOrder.filter((id) => id !== input.value);
        }
        clearCaptainSelectionError();
    }

    function showCaptainSelectionError(selectedCount) {
        const error = byID("captain-selection-error");
        const missing = gameState.questSize - selectedCount;
        error.textContent = `Select ${missing} more player${missing === 1 ? "" : "s"} before submitting. ${selectedCount} of ${gameState.questSize} selected.`;
        error.hidden = false;
        byID("captain-controls").classList.add("selection-error");
    }

    function clearCaptainSelectionError() {
        byID("captain-selection-error").hidden = true;
        byID("captain-controls").classList.remove("selection-error");
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
        renderQuestTeam();
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

    function renderQuestTeam() {
        const list = byID("quest-team");
        const submitted = new Set(gameState.submittedQuestPlayers || []);
        list.replaceChildren();

        for (const player of gameState.quest) {
            const hasSubmitted = submitted.has(player.id);
            const item = document.createElement("li");
            item.className = `quest-player-tile ${hasSubmitted ? "submitted" : "waiting"}`;

            const name = document.createElement("strong");
            name.textContent = player.id === playerID ? `${player.name} (you)` : player.name;
            const state = document.createElement("span");
            state.textContent = hasSubmitted ? "Card chosen" : "Choosing card…";

            item.append(name, state);
            list.append(item);
        }
    }

    function renderTeam(list, team) {
        list.replaceChildren();
        for (const player of team) {
            const item = document.createElement("li");
            item.textContent = player.id === playerID ? `${player.name} (you)` : player.name;
            list.append(item);
        }
    }

    function renderQuestCards(list) {
        const resultsByRound = new Map((gameState.questResults || []).map((result) => [result.round, result]));
        list.replaceChildren();

        for (let round = 1; round <= gameState.totalRounds; round++) {
            const result = resultsByRound.get(round);
            const status = result ? (result.succeeded ? "succeeded" : "failed") : "pending";
            const statusLabel = result?.automatic
                ? "Auto-failed"
                : status === "pending" ? "Not played" : status === "succeeded" ? "Succeeded" : "Failed";
            const icon = status === "pending" ? "○" : status === "succeeded" ? "✓" : "✕";

            const card = document.createElement("li");
            card.className = `quest-card ${status}`;
            card.setAttribute("aria-label", `Quest ${round}: ${statusLabel}`);

            const roundLabel = document.createElement("span");
            roundLabel.className = "quest-card-round";
            roundLabel.textContent = `Quest ${round}`;

            const iconElement = document.createElement("span");
            iconElement.className = "quest-card-icon";
            iconElement.setAttribute("aria-hidden", "true");
            iconElement.textContent = icon;

            const statusElement = document.createElement("span");
            statusElement.className = "quest-card-status";
            statusElement.textContent = statusLabel;

            const teamSize = document.createElement("span");
            teamSize.className = "quest-card-team-size";
            const requiredPlayers = gameState.questSizes?.[round - 1] || (round === gameState.round ? gameState.questSize : 0);
            teamSize.textContent = requiredPlayers ? `${requiredPlayers} player${requiredPlayers === 1 ? "" : "s"}` : "";

            card.append(roundLabel, iconElement, statusElement, teamSize);
            list.append(card);
        }
    }

    function renderVoteFailureTracker() {
        renderVoteFailureTrackerFor(byID("vote-failure-tracker"));
    }

    function renderVoteFailureTrackerFor(
        tracker,
        failures = gameState.rejectedProposals || 0,
        limit = gameState.proposalRejectLimit || 5,
    ) {
        tracker.replaceChildren();
        tracker.setAttribute("aria-label", `${failures} of ${limit} team proposals rejected`);

        for (let attempt = 1; attempt <= limit; attempt++) {
            const marker = document.createElement("li");
            marker.className = `vote-failure-marker${attempt <= failures ? " filled" : ""}`;
            marker.textContent = String(attempt);
            marker.setAttribute("aria-label", `Rejection ${attempt}${attempt <= failures ? ": recorded" : ": empty"}`);
            tracker.append(marker);
        }
    }

    function renderEndedGame() {
        showOnly(endedView);
        const innocentsWon = gameState.winner === "innocent";
        const playerWon = Boolean(role) && role === gameState.winner;
        endedView.classList.toggle("winning", playerWon);
        endedView.classList.toggle("losing", Boolean(role) && !playerWon);
        endedView.classList.toggle("spectating", !role);
        byID("winner-message").textContent = innocentsWon ? "Innocents win!" : "Traitor wins!";
        byID("personal-result").textContent = !role
            ? "You watched this game as a spectator."
            : playerWon ? "Your team won" : "Your team lost";
        byID("victory-reason").textContent = innocentsWon
            ? "The innocents completed three successful quests."
            : "Three quests failed, giving the traitor the victory.";
        byID("traitor-name").textContent = gameState.traitors.map((player) => player.name).join(", ");
        renderQuestCards(byID("final-quest-cards"));
        byID("final-score").textContent = `${gameState.successfulQuests} successful quests · ${gameState.failedQuests} failed quests`;
        startForm.hidden = !isHost;
        if (isHost) endedView.append(startForm);
    }

    function resetToWaiting(message) {
        window.clearInterval(gameStartCountdownTimer);
        gameStartCountdownActive = false;
        gameState = null;
        role = "";
        roleConfirmed = false;
        pendingRoleConfirmations = [];
        dismissQuestResult(true);
        deferredQuestResult = null;
        dismissProposalResult();
        renderRoleConfirmation();
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
