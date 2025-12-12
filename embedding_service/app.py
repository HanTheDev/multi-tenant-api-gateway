from flask import Flask, request, jsonify
from sentence_transformers import SentenceTransformer
import numpy as np

app = Flask(__name__)
model = SentenceTransformer('all-MiniLM-L6-v2')

@app.route('/embed', methods=['POST'])
def embed():
    data = request.json
    text = data.get('text', '')
    
    embedding = model.encode(text)
    return jsonify({'embedding': embedding.tolist()})

@app.route('/similarity', methods=['POST'])
def similarity():
    data = request.json
    text1 = data.get('text1', '')
    text2 = data.get('text2', '')
    
    emb1 = model.encode(text1)
    emb2 = model.encode(text2)
    
    similarity = np.dot(emb1, emb2) / (np.linalg.norm(emb1) * np.linalg.norm(emb2))
    
    return jsonify({'similarity': float(similarity)})

if __name__ == '__main__':
    app.run(port=5000)