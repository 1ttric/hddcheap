import React, {Component} from 'react';
import './App.css';
import LinearProgress from "@material-ui/core/LinearProgress";
import MaterialTable from "material-table";
import {createTheme, Snackbar} from "@material-ui/core";
import MuiAlert from '@material-ui/lab/Alert';
import {ThemeProvider} from "@material-ui/styles";
import AppBar from "@material-ui/core/AppBar";
import Typography from "@material-ui/core/Typography";
import Toolbar from "@material-ui/core/Toolbar";
import Box from "@material-ui/core/Box";

let ws = new WebSocket(window.location.origin.replace(/^http/, 'ws') + "/ws");

class App extends Component {
    constructor(props) {
        super(props);
        this.state = {items: null, lastUpdate: null, updateSnackVisible: false};
    }

    componentDidMount() {
        ws.onopen = () => console.log("Websocket connected");
        ws.onmessage = (msg) => {
            let items = JSON.parse(msg.data);
            this.setState(state => ({
                ...state,
                items: items,
                lastUpdate: Date.now().toString(),
                updateSnackVisible: true
            }))
        }
    }

    renderLoading() {
        return (
            <LinearProgress/>
        )
    }

    renderLoaded() {
        // Customize some columns
        let columns = Object.keys(this.state.items[0]).filter(k => (k !== 'tableData')).map(k => {
                switch (k) {
                    case 'url':
                        // Make item URL clickable
                        return {title: k, field: k, render: rowData => <a href={rowData.url}>{rowData.url}</a>};
                    default:
                        return {title: k, field: k};
                }
            }
        );
        // Add camelcamelcamel price histories
        columns.push({
            'title': 'history',
            'field': 'history',
            render: rowData => <a href={"https://camelcamelcamel.com/product/" + rowData.asin} target={"_blank"}>
                <img alt={"Graphed price history from camelcamelcamel"} style={{mixBlendMode: 'multiply'}}
                     src={"https://charts.camelcamelcamel.com/us/" + rowData.asin + "/amazon-new-used.png?legend=1&fo=1&w=900"}/>
            </a>
        });

        return (
            <Box m={2}>
                <MaterialTable columns={columns}
                               data={this.state.items}
                               title={null}
                />
                <Snackbar open={this.state.updateSnackVisible}
                          onClose={() => this.setState(state => ({...state, updateSnackVisible: false}))}
                          autoHideDuration={2000}>
                    <MuiAlert severity="info" elevation={6}
                              variant="filled">{"Updated " + (new Date()).toTimeString()}</MuiAlert>
                </Snackbar>
            </Box>
        );
    }


    render() {
        return (
            <ThemeProvider theme={createTheme()}>
                <AppBar position="static">
                    <Toolbar>
                        <Typography variant="h6">
                            hdd.cheap
                        </Typography>
                    </Toolbar>
                </AppBar>
                <div>
                    {this.state.items === null ? this.renderLoading() : this.renderLoaded()}
                </div>
            </ThemeProvider>
        )
    }
}

export default App;
