// Ce fichier JS ajoute dynamiquement le bouton SAML sur la page de login si l'option SAML est active
fetch('/api/v1/saml/enabled')
  .then(r => r.json())
  .then(data => {
    if (data.enabled) {
      const btn = document.createElement('a');
      btn.className = 'btn btn-primary btn-block mt-3';
      btn.href = '/web/login/saml';
      btn.innerText = 'SAML login';
      document.getElementById('loginForm').appendChild(btn);
    }
  });
