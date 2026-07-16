(() => {
    const appBaseURL = new URL(document.baseURI);
    const storagePrefix = appBaseURL.pathname === "/" ? "" : `app:${appBaseURL.pathname}:`;
    const roomID = decodeURIComponent(window.location.pathname.split("/").pop());
    const byID = (id) => document.querySelector(`#${id}`);
    const joinPanel = byID("join-panel");
    const presencePanel = byID("presence-panel");
    const presencePanelHome = byID("presence-panel-home");
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
    const roleReveal = byID("role-reveal");
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
    const roundResult = byID("round-result");
    const gameError = byID("game-error");
    const sidebar = byID("room-sidebar");
    const sidebarToggle = byID("sidebar-toggle");
    const captainSidebarToggle = byID("captain-sidebar-toggle");
    const gameStartingSidebarToggle = byID("game-starting-sidebar-toggle");
    const sidebarClose = byID("sidebar-close");
    const sidebarBackdrop = byID("sidebar-backdrop");
    const endGameButton = byID("end-game");
    const endGameDialog = byID("end-game-dialog");
    const cancelEndGame = byID("cancel-end-game");
    const confirmEndGame = byID("confirm-end-game");
    const leaveRoomButton = byID("leave-room");
    const leaveRoomDialog = byID("leave-room-dialog");
    const cancelLeaveRoom = byID("cancel-leave-room");
    const confirmLeaveRoom = byID("confirm-leave-room");

    const participants = new Map();
    const autoJoinKey = `${storagePrefix}room-auto-join:${roomID}`;
    const roomDisplayNameKey = `${storagePrefix}room-display-name:${roomID}`;
    const presenceDisplayNameKey = `${storagePrefix}presence-display-name`;
    const tabTokenKey = `${storagePrefix}room-tab-token:${roomID}`;
    let socket;
    let reconnectTimer;
    let reconnectAttempts = 0;
    let chosenName = "";
    let intentionallyClosed = false;
    let sidebarReturnFocus = sidebarToggle;
    let isHost = false;
    let playerID = "";
    let role = "";
    let roleRevealed = false;
    let roleConfirmed = false;
    let pendingRoleConfirmations = [];
    let pendingGameStartConfirmations = [];
    let gameStartPlayers = [];
    let gameStarting = false;
    let gameStartConfirmed = false;
    let gameStartCountdownActive = false;
    let gameStartCountdownSeconds = 0;
    let gameStartCountdownTimer;
    let gameStartPulseTimer;
    let unreadyGlowTimer;
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
    let captainLayoutFrame;
    let rejectedTeamToastKey = "";
    let rejectedTeamToastTimer;
    let rejectedTeamToastHideTimer;
    let rejectedTeamToastExitAnimation;

    const storedDisplayName = window.sessionStorage.getItem(roomDisplayNameKey)
        || window.sessionStorage.getItem(presenceDisplayNameKey)
        || "";
    displayName.value = storedDisplayName;

    sidebarToggle.addEventListener("click", openSidebar);
    captainSidebarToggle.addEventListener("click", openSidebar);
    gameStartingSidebarToggle.addEventListener("click", openSidebar);
    sidebarClose.addEventListener("click", closeSidebar);
    sidebarBackdrop.addEventListener("click", closeSidebar);
    document.addEventListener("keydown", (event) => {
        if (event.key === "Escape" && sidebar.classList.contains("open")) closeSidebar();
    });
    window.addEventListener("resize", scheduleCaptainPlayerLayout);
    endGameButton.addEventListener("click", () => {
        closeSidebar(false);
        endGameDialog.showModal();
    });
    cancelEndGame.addEventListener("click", () => endGameDialog.close());
    confirmEndGame.addEventListener("click", () => {
        send({ type: "end_game" });
        endGameDialog.close();
    });
    endGameDialog.addEventListener("click", (event) => {
        if (event.target === endGameDialog) endGameDialog.close();
    });
    roleReveal.addEventListener("click", () => {
        roleRevealed = !roleRevealed;
        renderRole();
    });
    leaveRoomButton.addEventListener("click", () => {
        closeSidebar(false);
        leaveRoomDialog.showModal();
    });
    cancelLeaveRoom.addEventListener("click", () => leaveRoomDialog.close());
    confirmLeaveRoom.addEventListener("click", () => {
        intentionallyClosed = true;
        socket?.close();
        window.location.assign(appBaseURL.href);
    });
    leaveRoomDialog.addEventListener("click", (event) => {
        if (event.target === leaveRoomDialog) leaveRoomDialog.close();
    });

    joinForm.addEventListener("submit", (event) => {
        event.preventDefault();
        chosenName = displayName.value.trim();
        if (!chosenName) return;
        window.sessionStorage.setItem(presenceDisplayNameKey, chosenName);
        window.sessionStorage.setItem(roomDisplayNameKey, chosenName);
        window.sessionStorage.setItem(autoJoinKey, "true");
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
        const wasReady = gameStartConfirmed;
        gameStartConfirmed = !gameStartConfirmed;
        send({ type: "confirm_game_start" });
        renderGameStarting();
        if (wasReady) showUnreadyGlow();
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
    document.addEventListener("click", () => {
        if (!roundResult.hidden && roundResult.classList.contains("team-rejected-toast")) dismissRejectedTeamToast();
    });

    async function connect() {
        setStatus("Connecting…", false);
        let tabToken;
        try {
            tabToken = await ensureTabToken();
        } catch (error) {
            setStatus(error.message, false);
            joinPanel.hidden = false;
            presencePanel.hidden = true;
            return;
        }
        const url = new URL(`ws/rooms/${encodeURIComponent(roomID)}`, appBaseURL);
        url.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
        url.searchParams.set("name", chosenName);
        socket = new WebSocket(url, [`lobby-tab-token.${tabToken}`]);

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
                const currentParticipant = participants.get(playerID);
                if (currentParticipant) {
                    chosenName = currentParticipant.name;
                    window.sessionStorage.setItem(roomDisplayNameKey, chosenName);
                }
                role = event.data.role || "";
                gameState = event.data.game || null;
                roleConfirmed = Boolean(event.data.roleConfirmed);
                pendingRoleConfirmations = event.data.pendingRoleConfirmations || [];
                pendingProposalConfirmations = event.data.pendingProposalConfirmations || [];
                proposalResultConfirmed = Boolean(event.data.proposalResultConfirmed);
                submittedProposalVote = Boolean(event.data.proposalVoteSubmitted);
                submittedQuestCard = Boolean(event.data.questCardSubmitted);
                gameStarting = Boolean(event.data.gameStarting);
                pendingGameStartConfirmations = event.data.pendingGameStartConfirmations || [];
                gameStartPlayers = event.data.gameStartPlayers || [];
                gameStartConfirmed = Boolean(event.data.gameStartConfirmed);
                phaseKey = gameState
                    ? `${gameState.round}:${gameState.phase}:${gameState.captain?.id || ""}`
                    : "";
                renderGame();
                renderGameStarting();
                if (pendingProposalConfirmations.length && gameState?.lastProposal) {
                    announceProposalResult(gameState.lastProposal, gameState, proposalResultConfirmed);
                }
            } else if (event.type === "user_joined") {
                participants.set(event.data.id, event.data);
            } else if (event.type === "user_left") {
                participants.delete(event.data.id);
            } else if (event.type === "game_started") {
                // Keep the starting screen covering the room until the following
                // role_assigned event is ready to replace it. WebSocket messages
                // are separate tasks, so hiding it here can expose an intermediate
                // game view for a frame.
                gameStarting = true;
                gameStartConfirmed = false;
                pendingGameStartConfirmations = [];
                renderGameStarting();
                role = "";
                roleRevealed = false;
                roleConfirmed = false;
                pendingRoleConfirmations = event.data.players || [];
                setGameState(event.data);
            } else if (event.type === "role_assigned") {
                role = event.data.role;
                roleRevealed = false;
                gameStarting = false;
                stopGameStartCountdown();
                gameStartPlayers = [];
                renderGameStarting();
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
                gameStartConfirmed = !pendingGameStartConfirmations.some((player) => player.id === playerID);
                if (event.data.countdown) startGameCountdown();
                else if (gameStartCountdownActive) stopGameStartCountdown();
                renderGameStarting();
                if (event.data.unreadiedPlayer) showPlayerUnreadied(event.data.unreadiedPlayer.id);
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

    async function ensureTabToken() {
        const storedToken = window.sessionStorage.getItem(tabTokenKey);
        if (storedToken) return storedToken;

        const response = await fetch(new URL(`api/lobbies/${encodeURIComponent(roomID)}/tab-session`, appBaseURL), {
            method: "POST",
            headers: {"Content-Type": "application/json"}
        });
        if (!response.ok) throw new Error("Could not create a session for this tab.");
        const {token} = await response.json();
        if (!token) throw new Error("Could not create a session for this tab.");
        window.sessionStorage.setItem(tabTokenKey, token);
        return token;
    }

    function send(command) {
        gameError.textContent = "";
        if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(command));
    }

    function openSidebar(event) {
        sidebarReturnFocus = event?.currentTarget || sidebarToggle;
        sidebar.classList.add("open");
        sidebar.setAttribute("aria-hidden", "false");
        sidebarToggle.setAttribute("aria-expanded", "true");
        captainSidebarToggle.setAttribute("aria-expanded", "true");
        gameStartingSidebarToggle.setAttribute("aria-expanded", "true");
        sidebarBackdrop.hidden = false;
        document.body.classList.add("sidebar-open");
        sidebarClose.focus();
    }

    function closeSidebar(returnFocus = true) {
        sidebar.classList.remove("open");
        sidebar.setAttribute("aria-hidden", "true");
        sidebarToggle.setAttribute("aria-expanded", "false");
        captainSidebarToggle.setAttribute("aria-expanded", "false");
        gameStartingSidebarToggle.setAttribute("aria-expanded", "false");
        sidebarBackdrop.hidden = true;
        document.body.classList.remove("sidebar-open");
        if (returnFocus) sidebarReturnFocus.focus();
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
            dismissProposalResult(false);
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

    function announceProposalResult(result, state = gameState, alreadyConfirmed = false) {
        window.clearTimeout(proposalResultRevealTimer);
        window.clearInterval(proposalResultCountdownTimer);
        proposalResultConfirmed = alreadyConfirmed;
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

    function dismissProposalResult(showRejectedToast = true) {
        window.clearInterval(proposalResultCountdownTimer);
        window.clearInterval(proposalResultRevealTimer);
        proposalResultAnnouncement.hidden = true;
        if (deferredQuestResult) {
            const quest = deferredQuestResult;
            deferredQuestResult = null;
            announceQuestResult(quest);
        } else if (showRejectedToast) {
            showRejectedTeamToast();
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
        updateEndGameVisibility();
        updatePresencePanelLocation();
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
        const countdown = byID("game-starting-countdown");
        const readyMessage = byID("game-starting-ready-message");
        gameStartingView.hidden = !shouldShow;
        gameStartingView.classList.toggle("player-ready", gameStarting && gameStartConfirmed);
        readyMessage.textContent = gameStartCountdownActive ? "Everyone has readied up, the game will start shortly." : "Ready up to start";
        if (!shouldShow) return;

        const pendingIDs = new Set(pendingGameStartConfirmations.map((player) => player.id));
        const list = byID("game-starting-players");
        list.replaceChildren();
        for (const player of gameStartPlayers) {
            const item = document.createElement("li");
            item.dataset.playerId = player.id;
            const ready = !pendingIDs.has(player.id);
            item.classList.toggle("ready", ready);
            item.textContent = `${player.name}${player.id === playerID ? " (you)" : ""} · ${ready ? "Ready" : "Waiting"}`;
            list.append(item);
        }

        if (gameStartCountdownActive) {
            byID("game-starting-title").textContent = "Game starting";
            countdown.hidden = false;
            countdown.textContent = String(Math.max(gameStartCountdownSeconds, 1));
            byID("game-starting-players").hidden = false;
            gameStartingReady.hidden = false;
            gameStartingReady.textContent = "Unready";
            byID("game-starting-status").textContent = gameStartCountdownSeconds > 0
                ? `Starting in ${gameStartCountdownSeconds}…`
                : "Dealing roles…";
            return;
        }

        countdown.hidden = true;
        byID("game-starting-title").textContent = "Game starting";
        byID("game-starting-players").hidden = false;
        gameStartingReady.hidden = false;
        gameStartingReady.disabled = false;
        gameStartingReady.textContent = gameStartConfirmed ? "Ready!" : "Ready up";
        const remaining = pendingGameStartConfirmations.length;
        byID("game-starting-status").textContent = remaining
            ? `Waiting for ${remaining} player${remaining === 1 ? "" : "s"}.`
            : "Everyone is ready. Dealing roles…";
    }

    function showUnreadyGlow() {
        window.clearTimeout(unreadyGlowTimer);
        gameStartingView.classList.remove("player-unready");
        void gameStartingView.offsetWidth;
        gameStartingView.classList.add("player-unready");
        unreadyGlowTimer = window.setTimeout(() => {
            gameStartingView.classList.remove("player-unready");
        }, 700);
    }

    function showPlayerUnreadied(id) {
        const tile = byID("game-starting-players").querySelector(`[data-player-id="${CSS.escape(id)}"]`);
        if (!tile) return;
        tile.classList.remove("just-unreadied");
        void tile.offsetWidth;
        tile.classList.add("just-unreadied");
        window.setTimeout(() => tile.classList.remove("just-unreadied"), 1800);
    }

    function startGameCountdown() {
        window.clearInterval(gameStartCountdownTimer);
        gameStartCountdownActive = true;
        gameStartCountdownSeconds = 3;
        renderGameStarting();
        showCountdownPulse();
        gameStartCountdownTimer = window.setInterval(() => {
            gameStartCountdownSeconds -= 1;
            if (gameStartCountdownSeconds <= 0) {
                window.clearInterval(gameStartCountdownTimer);
                renderGameStarting();
                return;
            }
            renderGameStarting();
            showCountdownPulse();
        }, 1000);
    }

    function stopGameStartCountdown() {
        window.clearInterval(gameStartCountdownTimer);
        gameStartCountdownActive = false;
        gameStartCountdownSeconds = 0;
    }

    function showCountdownPulse() {
        window.clearTimeout(gameStartPulseTimer);
        gameStartingView.classList.remove("countdown-pulse");
        void gameStartingView.offsetWidth;
        gameStartingView.classList.add("countdown-pulse");
        gameStartPulseTimer = window.setTimeout(() => {
            gameStartingView.classList.remove("countdown-pulse");
        }, 550);
    }

    function renderRole() {
        const isPlayer = gameState?.players?.some((player) => player.id === playerID);
        const assignedRole = role || (isPlayer ? "Assigning…" : "Spectator");
        roleElement.textContent = roleRevealed ? assignedRole : "Reveal Secret Role";
        roleReveal.classList.toggle("revealed", roleRevealed);
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
        const result = roundResult;
        if (gameState.lastQuest) {
            dismissRejectedTeamToast(true);
            rejectedTeamToastKey = "";
            const quest = gameState.lastQuest;
            result.textContent = quest.automatic
                ? `Round ${quest.round} automatically failed after five rejected teams.`
                : quest.succeeded
                ? `Round ${quest.round} succeeded: all ${quest.successCards} cards were successes.`
                : `Round ${quest.round} failed: ${quest.failCards} fail card${quest.failCards === 1 ? "" : "s"} revealed.`;
            result.className = `round-result ${quest.succeeded ? "succeeded" : "failed"}`;
            result.hidden = false;
        } else if (gameState.lastProposal && !gameState.lastProposal.approved) {
            if (proposalResultAnnouncement.hidden && pendingProposalConfirmations.length === 0) showRejectedTeamToast();
            else result.hidden = true;
        } else {
            dismissRejectedTeamToast();
        }
    }

    function showRejectedTeamToast() {
        if (!gameState?.lastProposal || gameState.lastProposal.approved || gameState.lastQuest) return;
        const toastKey = `${gameState.round}:${gameState.rejectedProposals}:${gameState.lastProposal.yes}:${gameState.lastProposal.no}`;
        if (rejectedTeamToastKey === toastKey) return;
        rejectedTeamToastKey = toastKey;
        window.clearTimeout(rejectedTeamToastHideTimer);
        rejectedTeamToastExitAnimation?.cancel();
        rejectedTeamToastExitAnimation = null;
        roundResult.textContent = `Team rejected (${gameState.lastProposal.yes} yes, ${gameState.lastProposal.no} no). The captain has rotated.`;
        roundResult.className = "round-result failed team-rejected-toast";
        roundResult.hidden = false;
        window.clearTimeout(rejectedTeamToastTimer);
        rejectedTeamToastTimer = window.setTimeout(dismissRejectedTeamToast, 5000);
    }

    function dismissRejectedTeamToast(immediately = false) {
        window.clearTimeout(rejectedTeamToastTimer);
        window.clearTimeout(rejectedTeamToastHideTimer);
        if (!roundResult.classList.contains("team-rejected-toast")) return;
        if (immediately) {
            rejectedTeamToastExitAnimation?.cancel();
            rejectedTeamToastExitAnimation = null;
            roundResult.hidden = true;
            return;
        }
        if (rejectedTeamToastExitAnimation) return;

        const exitDistance = roundResult.offsetHeight + 32;
        const animation = roundResult.animate([
            { opacity: 1, transform: "translateY(0)" },
            { opacity: 0, transform: `translateY(${exitDistance}px)` },
        ], {
            duration: 300,
            easing: "ease-in",
            fill: "forwards",
        });
        rejectedTeamToastExitAnimation = animation;

        const finishDismissal = () => {
            if (rejectedTeamToastExitAnimation !== animation) return;
            rejectedTeamToastExitAnimation = null;
            roundResult.hidden = true;
            animation.cancel();
        };
        animation.finished.then(finishDismissal).catch(() => {});
        rejectedTeamToastHideTimer = window.setTimeout(finishDismissal, 400);
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

        renderQuestCards(byID("captain-quest-cards"));
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
        updateCaptainPlayerLayout();
    }

    function scheduleCaptainPlayerLayout() {
        window.cancelAnimationFrame(captainLayoutFrame);
        captainLayoutFrame = window.requestAnimationFrame(updateCaptainPlayerLayout);
    }

    function updateCaptainPlayerLayout() {
        const panel = byID("quest-team-options");
        if (panel.hidden || panel.offsetParent === null) return;
        panel.classList.remove("double-stacked");
        if (panel.scrollHeight > panel.clientHeight) panel.classList.add("double-stacked");
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
        scheduleCaptainPlayerLayout();
    }

    function clearCaptainSelectionError() {
        byID("captain-selection-error").hidden = true;
        byID("captain-controls").classList.remove("selection-error");
        scheduleCaptainPlayerLayout();
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
        gameStarting = false;
        gameStartConfirmed = false;
        pendingGameStartConfirmations = [];
        gameStartPlayers = [];
        renderGameStarting();
        gameState = null;
        updateEndGameVisibility();
        updatePresencePanelLocation();
        if (endGameDialog.open) endGameDialog.close();
        role = "";
        roleRevealed = false;
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

    function updateEndGameVisibility() {
        endGameButton.hidden = !(isHost && gameState?.phase && gameState.phase !== "complete");
    }

    function updatePresencePanelLocation() {
        const gameIsActive = Boolean(gameState?.phase && gameState.phase !== "complete");
        document.body.classList.toggle("gameplay-active", gameIsActive);
        const destination = gameIsActive ? sidebar : presencePanelHome;
        if (presencePanel.parentElement !== destination) destination.append(presencePanel);
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
        for (const player of gameState?.players || []) {
            if (participants.has(player.id)) continue;
            const item = document.createElement("li");
            item.className = "participant-offline";
            item.textContent = `${player.name} · disconnected`;
            participantList.append(item);
        }
        participantCount.textContent = String(participants.size);
        gamePanel.hidden = false;
        if (!gameState) startForm.hidden = !isHost;
        waitingMessage.textContent = isHost ? "Start when at least three players are ready." : "Waiting for the host to start a game.";
        nextGameMessage.textContent = isHost ? "Start a new game when everyone is ready." : "Waiting for the host to start a new game.";
    }

    if (storedDisplayName && window.sessionStorage.getItem(autoJoinKey) === "true") {
        chosenName = storedDisplayName;
        joinPanel.hidden = true;
        presencePanel.hidden = false;
        connect();
    }
})();
