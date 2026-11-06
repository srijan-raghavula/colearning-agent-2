(function () {
  "use strict";

  var root = document.getElementById("learningStudio");
  if (!root) return;

  var courseID = root.dataset.courseId;
  var chapterID = root.dataset.chapterId;
  var csrf = root.dataset.csrf;
  var lessonStatus = document.getElementById("lessonStatus");
  var lessonContent = document.getElementById("lessonContent");
  var retryLesson = document.getElementById("retryLesson");
  var form = document.getElementById("tutorForm");
  var question = document.getElementById("tutorQuestion");
  var sendButton = document.getElementById("sendQuestion");
  var messages = document.getElementById("chatMessages");
  var chatError = document.getElementById("chatError");
  var quizStatus = document.getElementById("quizStatus");
  var quizForm = document.getElementById("quickQuiz");
  var quizQuestions = document.getElementById("quizQuestions");
  var quizResult = document.getElementById("quizResult");
  var retryQuiz = document.getElementById("retryQuiz");
  var quizID = "";
  var quizData = [];
  var history = [];

  function endpoint(action) {
    return "/student/courses/" + encodeURIComponent(courseID) + "/chapters/" +
      encodeURIComponent(chapterID) + "/" + action;
  }

  function postJSON(action, data) {
    return fetch(endpoint(action), {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": csrf,
        "Accept": "application/json"
      },
      body: JSON.stringify(data || {})
    }).then(function (response) {
      return response.json().catch(function () {
        throw new Error("The tutor response could not be read. Please try again.");
      }).then(function (body) {
        if (!response.ok) throw new Error(body.error || "The tutor is unavailable. Please try again.");
        return body;
      });
    });
  }

  function appendInline(parent, text) {
    var pattern = /(\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*)/g;
    var last = 0;
    var match;
    while ((match = pattern.exec(text)) !== null) {
      if (match.index > last) parent.appendChild(document.createTextNode(text.slice(last, match.index)));
      var token = match[0];
      var el;
      if (token.slice(0, 2) === "**") {
        el = document.createElement("strong");
        el.textContent = token.slice(2, -2);
      } else if (token.charAt(0) === "`") {
        el = document.createElement("code");
        el.textContent = token.slice(1, -1);
      } else {
        el = document.createElement("em");
        el.textContent = token.slice(1, -1);
      }
      parent.appendChild(el);
      last = pattern.lastIndex;
    }
    if (last < text.length) parent.appendChild(document.createTextNode(text.slice(last)));
  }

  function renderMarkdown(container, markdown) {
    container.replaceChildren();
    var lines = markdown.replace(/\r\n/g, "\n").split("\n");
    var paragraph = [];
    var list = null;
    var code = null;

    function flushParagraph() {
      if (!paragraph.length) return;
      var p = document.createElement("p");
      paragraph.forEach(function (line, index) {
        if (index) p.appendChild(document.createElement("br"));
        appendInline(p, line);
      });
      container.appendChild(p);
      paragraph = [];
    }

    function closeList() {
      if (list) {
        container.appendChild(list);
        list = null;
      }
    }

    lines.forEach(function (line) {
      if (line.trim().slice(0, 3) === "```") {
        flushParagraph();
        closeList();
        if (code) {
          var pre = document.createElement("pre");
          var codeEl = document.createElement("code");
          codeEl.textContent = code.join("\n");
          pre.appendChild(codeEl);
          container.appendChild(pre);
          code = null;
        } else {
          code = [];
        }
        return;
      }
      if (code) {
        code.push(line);
        return;
      }
      var heading = /^(#{1,3})\s+(.+)$/.exec(line);
      var bullet = /^\s*[-*]\s+(.+)$/.exec(line);
      var numbered = /^\s*\d+[.)]\s+(.+)$/.exec(line);
      if (!line.trim()) {
        flushParagraph();
        closeList();
        return;
      }
      if (heading) {
        flushParagraph();
        closeList();
        var h = document.createElement("h" + Math.min(heading[1].length + 2, 4));
        appendInline(h, heading[2]);
        container.appendChild(h);
      } else if (bullet || numbered) {
        flushParagraph();
        var ordered = Boolean(numbered);
        if (!list || list.tagName.toLowerCase() !== (ordered ? "ol" : "ul")) {
          closeList();
          list = document.createElement(ordered ? "ol" : "ul");
        }
        var li = document.createElement("li");
        appendInline(li, (bullet || numbered)[1]);
        list.appendChild(li);
      } else {
        closeList();
        paragraph.push(line);
      }
    });
    if (code) {
      var pre = document.createElement("pre");
      var codeEl = document.createElement("code");
      codeEl.textContent = code.join("\n");
      pre.appendChild(codeEl);
      container.appendChild(pre);
    }
    flushParagraph();
    closeList();
  }

  function addBubble(role, content, loading) {
    var bubble = document.createElement("article");
    bubble.className = "chat-bubble " + (role === "user" ? "chat-bubble-user" : "chat-bubble-ai");
    var label = document.createElement("span");
    label.className = "chat-bubble-label";
    label.textContent = role === "user" ? "YOU" : "AI TUTOR";
    bubble.appendChild(label);
    var body = document.createElement("div");
    body.className = "chat-bubble-content";
    if (loading) {
      body.classList.add("chat-loading");
      body.textContent = "Thinking";
      body.setAttribute("aria-label", "AI tutor is generating a response");
    } else if (role === "user") {
      body.textContent = content;
    } else {
      renderMarkdown(body, content);
    }
    bubble.appendChild(body);
    messages.appendChild(bubble);
    messages.scrollTop = messages.scrollHeight;
    return bubble;
  }

  function loadLesson() {
    lessonStatus.className = "lesson-status";
    lessonStatus.textContent = "Preparing a clear, chapter-based explanation…";
    lessonContent.hidden = true;
    retryLesson.hidden = true;
    postJSON("lesson").then(function (result) {
      renderMarkdown(lessonContent, result.response);
      lessonContent.hidden = false;
      lessonStatus.hidden = true;
    }).catch(function (error) {
      lessonStatus.hidden = false;
      lessonStatus.className = "lesson-status is-error";
      lessonStatus.textContent = error.message;
      retryLesson.hidden = false;
    });
  }

  function loadQuiz() {
    quizStatus.hidden = false;
    quizStatus.className = "quiz-status";
    quizStatus.textContent = "Creating three questions from this chapter…";
    quizForm.hidden = true;
    quizResult.hidden = true;
    retryQuiz.hidden = true;
    quizID = "";
    quizData = [];
    postJSON("quiz").then(function (result) {
      quizID = result.quiz_id;
      quizData = result.questions;
      quizQuestions.replaceChildren();
      quizData.forEach(function (item, index) {
        var fieldset = document.createElement("fieldset");
        fieldset.className = "quiz-question";
        var legend = document.createElement("legend");
        legend.textContent = (index + 1) + ". " + item.question;
        fieldset.appendChild(legend);
        item.options.forEach(function (option, optionIndex) {
          var label = document.createElement("label");
          label.className = "quiz-option";
          var input = document.createElement("input");
          input.type = "radio";
          input.name = "quiz-question-" + index;
          input.value = String(optionIndex);
          input.required = true;
          var text = document.createElement("span");
          text.textContent = option;
          label.appendChild(input);
          label.appendChild(text);
          fieldset.appendChild(label);
        });
        quizQuestions.appendChild(fieldset);
      });
      quizStatus.hidden = true;
      quizForm.hidden = false;
    }).catch(function (error) {
      quizStatus.className = "quiz-status is-error";
      quizStatus.textContent = error.message;
      retryQuiz.hidden = false;
    });
  }

  function retryCurrentQuiz() {
    if (!quizID || !quizData.length) {
      loadQuiz();
      return;
    }
    quizForm.reset();
    quizForm.hidden = false;
    quizResult.hidden = true;
    retryQuiz.hidden = true;
    quizStatus.hidden = true;
  }

  function submitQuiz(event) {
    event.preventDefault();
    var answers = [];
    for (var i = 0; i < quizData.length; i++) {
      var selected = quizForm.querySelector('input[name="quiz-question-' + i + '"]:checked');
      if (!selected) {
        quizStatus.className = "quiz-status is-error";
        quizStatus.textContent = "Choose an answer for each question before checking your score.";
        quizStatus.hidden = false;
        return;
      }
      answers.push(Number(selected.value));
    }
    var submit = document.getElementById("submitQuiz");
    submit.disabled = true;
    quizStatus.className = "quiz-status";
    quizStatus.textContent = "Checking your answers…";
    quizStatus.hidden = false;
    postJSON("quiz/submit", { quiz_id: quizID, answers: answers }).then(function (result) {
      quizStatus.hidden = true;
      quizForm.hidden = true;
      quizResult.replaceChildren();
      var score = document.createElement("div");
      score.className = "quiz-score";
      score.textContent = "You scored " + result.score + " out of " + result.total;
      quizResult.appendChild(score);
      if (result.total > 0 && result.score === result.total) {
        var congratulations = document.createElement("p");
        congratulations.className = "quiz-congratulations";
        congratulations.textContent = "Congratulations! You got a perfect score!";
        quizResult.appendChild(congratulations);
      }
      result.results.forEach(function (item, index) {
        var feedback = document.createElement("div");
        feedback.className = "quiz-feedback " + (item.correct ? "is-correct" : "is-incorrect");
        var title = document.createElement("b");
        title.textContent = (item.correct ? "Correct" : "Not quite") + " · Question " + (index + 1);
        var explanation = document.createElement("p");
        explanation.textContent = item.explanation;
        feedback.appendChild(title);
        feedback.appendChild(explanation);
        quizResult.appendChild(feedback);
      });
      quizResult.hidden = false;
      retryQuiz.hidden = result.total > 0 && result.score === result.total;
    }).catch(function (error) {
      quizStatus.className = "quiz-status is-error";
      quizStatus.textContent = error.message;
      quizStatus.hidden = false;
    }).finally(function () {
      submit.disabled = false;
    });
  }

  retryLesson.addEventListener("click", loadLesson);
  retryQuiz.addEventListener("click", retryCurrentQuiz);
  quizForm.addEventListener("submit", submitQuiz);
  if (root.dataset.aiReady === "true") {
    loadLesson();
    loadQuiz();
  }

  function sendQuestion(text) {
    var value = (text || question.value).trim();
    if (!value || sendButton.disabled) return;
    question.value = "";
    chatError.hidden = true;
    addBubble("user", value, false);
    var loading = addBubble("assistant", "", true);
    sendButton.disabled = true;
    question.disabled = true;
    postJSON("chat", { question: value, history: history.slice(-12) }).then(function (result) {
      loading.remove();
      addBubble("assistant", result.response, false);
      history.push({ role: "user", content: value }, { role: "assistant", content: result.response });
      history = history.slice(-8);
      var historyLength = history.reduce(function (total, message) {
        return total + message.content.length;
      }, 0);
      while (history.length && historyLength > 12000) {
        historyLength -= history.shift().content.length;
      }
    }).catch(function (error) {
      loading.remove();
      question.value = value;
      chatError.textContent = error.message;
      chatError.hidden = false;
    }).finally(function () {
      sendButton.disabled = false;
      question.disabled = false;
      question.focus();
    });
  }

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    sendQuestion();
  });
  question.addEventListener("keydown", function (event) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      sendQuestion();
    }
  });
  document.querySelectorAll(".suggestion-chip").forEach(function (button) {
    button.addEventListener("click", function () {
      sendQuestion(button.textContent);
    });
  });
})();
