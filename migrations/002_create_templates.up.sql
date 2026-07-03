CREATE TABLE IF NOT EXISTS templates (
    name VARCHAR(50) PRIMARY KEY,
    body TEXT NOT NULL
);

INSERT INTO templates (name, body) VALUES 
('WELCOME', 'Welcome to our service, {{name}}! We are glad to have you.'),
('MFA', 'Your security code is: {{code}}. This code is valid for 5 minutes.'),
('PASSWORD_RESET', 'Click the link to reset your password: {{link}}')
ON CONFLICT (name) DO NOTHING;
