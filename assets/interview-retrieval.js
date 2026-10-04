document.querySelectorAll('[data-reveal]').forEach((button) => {
  button.addEventListener('click', () => {
    const answer = document.getElementById(button.dataset.reveal)
    if (!answer) return
    answer.hidden = !answer.hidden
    button.textContent = answer.hidden ? '核对答案' : '收起答案'
  })
})
